package index

import (
	"context"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/robertlestak/ethindexer/internal/db"
	"github.com/robertlestak/ethindexer/internal/web3crypto"
	"github.com/robertlestak/ethindexer/internal/work"
	log "github.com/sirupsen/logrus"
)

var (
	checkedBlocks  = make(map[string]uint64)
	checkBlockLock sync.Mutex
)

func parseInputData(d []byte) (map[string]interface{}, error) {
	l := log.WithFields(log.Fields{
		"action": "parseInputData",
	})
	data := make(map[string]interface{})
	var err error
	l.Debug("start")
	defer l.Debug("end")
	if len(d) == 0 {
		return data, err
	}
	stx := hexutil.Encode(d)
	l.WithField("stx", stx).Debug("start")
	// erc20 send '0x' + 4 bytes 'a9059cbb' + 32 bytes (64 chars) for contract address and 32 bytes for its value
	if strings.HasPrefix(stx, "0xa9059cbb") && len(stx) > 74 {
		fc := stx[10 : 10+64]
		ls := "0x" + fc[len(fc)-40:]
		data["contract_to_addr"] = ls
		var ok bool
		data["contract_value"], ok = new(big.Int).SetString(stx[:74], 0)
		if !ok {
			l.WithError(err).Error("failed to parse contract value")
			return data, err
		}
	} else if strings.HasPrefix(stx, "0x755edd17") {
		// erc721 mintTo '0x' + '755edd17' + padding - last 40 are to addr
		data["contract_to_addr"] = "0x" + stx[len(stx)-40:]
	} else if strings.HasPrefix(stx, "0x23b872dd") {
		// erc721 transferFrom '0x' + '23b872dd' +
		// 32 bytes (64 chars) for from addr + 32 bytes (64 chars) for to addr
		// + 32 bytes (64 chars) for token id
		fc := stx[10+88:]
		data["contract_to_addr"] = "0x" + fc[0:40]
	}
	l.WithField("data", data).Debug("end")
	return data, err
}

// indexTransaction indexes a single transaction
func indexTransaction(ctx context.Context, c *ethclient.Client, blockNumber uint64, blockTime int64, tx types.Transaction, chain string) error {
	l := log.WithFields(log.Fields{
		"action":      "indexTransaction",
		"tx":          tx.Hash().Hex(),
		"chain":       chain,
		"blockNumber": blockNumber,
		"blockTime":   blockTime,
	})
	l.Debug("start")
	l.WithField("tx", tx.Hash().String()).Debug("start")
	l.Debugf("%+v", tx)
	var to string
	if tx.To() != nil {
		to = tx.To().String()
	}
	l.Debugf("GetTokenIDFromTransaction")
	receipt, rerr := c.TransactionReceipt(ctx, tx.Hash())
	if rerr != nil && rerr != ethereum.NotFound {
		l.WithError(rerr).Errorf("error getting transaction receipt")
		return rerr
	} else if rerr == ethereum.NotFound {
		l.Info("tx not found")
		return nil
	}
	t := Transaction{
		ChainID:     chain,
		Hash:        tx.Hash().Hex(),
		Time:        blockTime,
		Status:      receipt.Status,
		BlockNumber: int64(blockNumber),
		ToAddr:      to,
		Value:       tx.Value().Int64(),
		Gas:         int64(tx.Gas()),
		GasPrice:    tx.GasPrice().Int64(),
		Data:        tx.Data(),
	}
	l.Debugf("%+v", t)
	t.ChainHash = t.ChainID + "_" + t.Hash
	idata, ierr := parseInputData(t.Data)
	if ierr != nil {
		l.WithError(ierr).Error("failed to parse input data")
		return ierr
	}
	if tid, terr := TokenIDFromTransaction(c, receipt); terr == nil && tid != nil {
		t.ContractTokenID = tid.Int64()
	} else {
		l.WithError(terr).Debug("failed to get token id")
	}
	if len(idata) > 0 {
		if _, ok := idata["contract_to_addr"]; ok {
			t.ContractToAddr = idata["contract_to_addr"].(string)
		}
		if _, ok := idata["contract_value"]; ok {
			bi := idata["contract_value"].(*big.Int)
			t.ContractValue = bi.Uint64()
		}
	}
	chainID, ciderr := c.ChainID(ctx)
	if ciderr != nil {
		l.WithError(ciderr).Error("failed to get chain id")
		return ciderr
	}
	if msg, err := tx.AsMessage(types.LatestSignerForChainID(chainID), nil); err != nil {
		l.WithError(err).Debug("failed to get message")
	} else {
		t.FromAddr = msg.From().Hex()
	}
	err := t.Index()
	if err != nil {
		l.WithError(err).Error("failed to index transaction")
		//work.AddTransaction(chain, blockNumber, uint64(blockTime), tx, "right")
		return err
	}
	l.Debug("end")
	return nil
}

func IndexTx(chain string, tx types.Transaction, blockData string) error {
	txh := tx.Hash().Hex()
	l := log.WithFields(log.Fields{
		"action": "IndexTx",
		"tx":     txh,
	})
	l.Debug("start")
	defer l.Debug("end")

	bd := strings.Split(blockData, "_")
	blockNumbers := bd[0]
	blockTimes := bd[1]
	l.Debug("parse block data")
	blockNumber, err := strconv.ParseUint(blockNumbers, 10, 64)
	if err != nil {
		l.WithError(err).Error("failed to parse block number")
		return err
	}
	blockTime, err := strconv.ParseInt(blockTimes, 10, 64)
	if err != nil {
		l.WithError(err).Error("failed to parse block time")
		return err
	}
	l.Debug("get blockchain client")
	c, cerr := web3crypto.GetBlockchainClient(chain)
	if cerr != nil {
		l.WithError(cerr).Error("failed to get blockchain client")
		return cerr
	}
	l.Debug("index transaction")
	ierr := indexTransaction(context.Background(), c, blockNumber, blockTime, tx, chain)
	if ierr != nil {
		l.WithError(ierr).Error("failed to index transaction")
		return ierr
	}
	l.Debug("mark tx index complete")
	complete, icerr := work.IndexComplete(chain, blockNumbers, tx.Hash().String())
	if icerr != nil {
		l.WithError(icerr).Error("failed to mark index complete")
		return icerr
	}
	if complete {
		l.Debug("block index complete")
		cs := &ChainState{
			ChainID:          chain,
			LastCheckedBlock: blockNumber,
		}
		cs.IncrementLocalLastCheckedBlock()
		if os.Getenv("BACKFILL_ENABLED") == "true" {
			csb := &ChainStateBackfill{
				ChainID:          chain,
				LastCheckedBlock: blockNumber,
			}
			csb.UpdateLocalLastCheckedBlock()
		}
	}
	return nil
}

func indexOne(chain string, res chan error) {
	l := log.WithFields(log.Fields{
		"action": "indexOne",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	order := os.Getenv("WORKER_ORDER")
	tx, blockData, err := work.GetNextTx(chain, order)
	if err != nil {
		l.WithError(err).Error("failed to get next tx")
		res <- err
		return
	}
	if tx == nil {
		l.Debug("no tx to index")
		res <- nil
		return
	}
	l = l.WithField("tx", tx.Hash().Hex())
	ierr := IndexTx(chain, *tx, blockData)
	if ierr != nil {
		l.WithError(ierr).Error("failed to index job")
		res <- ierr
		return
	}
	res <- nil
}

func indexWorker(chain string) error {
	l := log.WithFields(log.Fields{
		"action": "indexWorker",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var jobsLen = 10
	if os.Getenv("TX_JOBS_LEN") != "" {
		var err error
		jobsLen, err = strconv.Atoi(os.Getenv("TX_JOBS_LEN"))
		if err != nil {
			l.WithError(err).Error("failed to parse TX_JOBS_LEN")
			return err
		}
	}
	res := make(chan error, jobsLen)
	for i := 0; i < jobsLen; i++ {
		go indexOne(chain, res)
	}
	for i := 0; i < jobsLen; i++ {
		select {
		case err := <-res:
			if err != nil {
				l.WithError(err).Error("failed to index")
				return err
			}
		case <-time.After(time.Second * 300):
			l.Error("timeout")
			return errors.New("timeout waiting for response")
		}
	}
	return nil
}

func indexBlockWorker(ctx context.Context, chain string, c *ethclient.Client, blocks chan uint64, res chan error) {
	l := log.WithFields(log.Fields{
		"action": "indexBlockWorker",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var err error
	for block := range blocks {
		l = l.WithField("block", block)
		l.Debug("indexing block")
		var retries = 0
		var retryCount = 10
	Retry:
		for retries < retryCount {
			err = indexBlockByNumber(ctx, c, chain, block)
			if err != nil {
				l.WithError(err).Error("failed to index block")
				retries++
				if retries >= retryCount {
					l.WithField("retries", retries).Error("failed to index block")
					res <- errors.New("failed to index block")
					return
				}
				time.Sleep(time.Duration(retries) * time.Second)
			} else {
				break Retry
			}
			retries++
		}
		res <- nil
	}
}

func indexBlockScheduler(ctx context.Context, chain string) {
	l := log.WithFields(log.Fields{
		"action": "indexBlockScheduler",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var jobsLen = 1000
	if os.Getenv("JOBS_LEN") != "" {
		var err error
		jobsLen, err = strconv.Atoi(os.Getenv("JOBS_LEN"))
		if err != nil {
			CleanShutdownWithError(err, "failed to parse JOBS_LEN")
		}
	}
	c, err := web3crypto.GetBlockchainClient(chain)
	if err != nil {
		CleanShutdownWithError(err, "failed to get blockchain client")
	}
	blocks := make(chan uint64, jobsLen)
	res := make(chan error, jobsLen)
	for i := 0; i < jobsLen; i++ {
		go indexBlockWorker(ctx, chain, c, blocks, res)
	}
	go func() {
		for {
			select {
			case err := <-res:
				if err != nil {
					l.WithError(err).Error("failed to index block")
					continue
				}
			case <-time.After(time.Second * 300):
				l.Error("timeout")
			}
		}
	}()
	interval := time.Millisecond * 1
	for {
		l.Debug("getting next block")
		block, err := work.GetNextBlock(chain)
		if err != nil {
			l.WithError(err).Error("failed to get next block")
			time.Sleep(interval)
			continue
		}
		if block == 0 {
			l.Debug("no block to index")
			time.Sleep(interval)
			continue
		}
		l.Debug("adding block to queue", "block", block)
		blocks <- block
		time.Sleep(interval)
	}
}

func indexBlockByNumber(ctx context.Context, c *ethclient.Client, chain string, blockNumber uint64) error {
	l := log.WithFields(log.Fields{
		"action": "indexBlockByNumber",
	})
	l.Debug("start")
	l.WithFields(
		log.Fields{
			"lastCheckedBlock": blockNumber,
		},
	).Debug("start")
	block, err := c.BlockByNumber(ctx, big.NewInt(int64(blockNumber)))
	if err != nil {
		l.WithError(err).Error("failed to get block")
		return err
	}
	l.WithField("block", block.NumberU64()).Debug("got block")
	if len(block.Transactions()) == 0 {
		l.Debug("no transactions to index")
		cs := &ChainState{
			ChainID:          chain,
			LastCheckedBlock: blockNumber,
		}
		if rerr := work.RemoveBlock(chain, blockNumber); rerr != nil {
			l.WithError(rerr).Error("failed to remove block")
			return rerr
		}
		cs.IncrementLastCheckedBlock()
		return nil
	}
	l.WithField("transactions", len(block.Transactions())).Debug("got transactions")
	txs := block.Transactions()
	l.Debugf("Adding tx to redis (%d)", len(txs))
	for _, tx := range txs {
		if err := indexTransaction(ctx, c, block.NumberU64(), int64(block.Time()), *tx, chain); err != nil {
			l.WithError(err).Error("failed to index transaction")
			return err
		}
	}
	cs := &ChainState{
		ChainID:          chain,
		LastCheckedBlock: blockNumber,
	}
	cs.IncrementLocalLastCheckedBlock()
	l.Debug("end")
	return nil
}

// indexProcess is the main index process for each chain
func indexProcess(ctx context.Context, chain string) error {
	l := log.WithFields(log.Fields{
		"action": "indexProcess",
		"chain":  chain,
	})
	l.Debug("start")
	chain = strings.TrimSpace(chain)
	if chain == "" {
		l.Error("chain is empty")
		return errors.New("chain is empty")
	}
	c, err := web3crypto.GetBlockchainClient(chain)
	if err != nil {
		l.WithError(err).Error("failed to get blockchain client")
		return err
	}
	endBlock, err := getEndBlock(ctx, c)
	if err != nil {
		l.WithError(err).Error("failed to get end block")
		return err
	}
	l = l.WithField("endBlock", endBlock)
	l.Debug("endBlock")
	var lastCheckedBlock uint64
	if _, ok := checkedBlocks[chain]; !ok {
		cs := &ChainState{
			ChainID: chain,
		}
		lastCheckedBlock = cs.GetLastSoftCheckedBlock()
		l = l.WithField("lastCheckedBlock", lastCheckedBlock)
		checkedBlocks[chain] = lastCheckedBlock
	} else {
		lastCheckedBlock = checkedBlocks[chain]
	}
	l.Debug("start indexing")
	lastCheckedBlock++
	blockStepDelay := time.Millisecond * 10
	if os.Getenv("BLOCK_STEP_DELAY") != "" {
		var err error
		blockStepDelay, err = time.ParseDuration(os.Getenv("BLOCK_STEP_DELAY"))
		if err != nil {
			CleanShutdownWithError(err, "failed to parse BLOCK_STEP_DELAY")
		}
	}
	for lastCheckedBlock < endBlock {
		if aerr := work.AddBlock(chain, lastCheckedBlock); aerr != nil {
			l.WithError(aerr).Error("failed to add block")
			return aerr
		}
		checkBlockLock.Lock()
		lastCheckedBlock++
		checkedBlocks[chain] = lastCheckedBlock
		checkBlockLock.Unlock()
		time.Sleep(blockStepDelay)
	}
	l.Debug("end")
	return nil
}

type BlockJob struct {
	ChainID     string
	BlockNumber uint64
}

func orphanScheduleWorker(ctx context.Context, bjs <-chan BlockJob, res chan<- error) {
	l := log.WithFields(log.Fields{
		"action": "orphanScheduleWorker",
	})
	l.Debug("start")
	defer l.Debug("end")
	for bj := range bjs {
		l = l.WithFields(log.Fields{
			"chainID":     bj.ChainID,
			"blockNumber": bj.BlockNumber,
		})
		l.Debug("start work")
		var err error
		l.Debug("start get block")
		c, cerr := web3crypto.GetBlockchainClient(bj.ChainID)
		if cerr != nil {
			CleanShutdownWithError(cerr, "failed to get blockchain client")
			res <- cerr
		}
		l.Debug("index block by number")
		if err = indexBlockByNumber(ctx, c, bj.ChainID, bj.BlockNumber); err != nil {
			CleanShutdownWithError(err, "failed to index orphaned block")
			res <- err
		}
		l.WithField("block", bj.BlockNumber).Debug("indexed orphaned block")
		res <- nil
		l.Debug("end work")
	}
}

func rescheduleOrphans(ctx context.Context, chain string) {
	l := log.WithFields(log.Fields{
		"action": "rescheduleOrphans",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	orphans, err := work.OrphanedBlocks(chain)
	if err != nil {
		l.WithError(err).Error("failed to get orphaned blocks")
	}
	l = l.WithField("orphans", len(orphans))
	jobs := make(chan BlockJob, len(orphans))
	res := make(chan error, len(orphans))
	wlen := len(orphans)
	if wlen == 0 {
		l.Debug("no orphans to reschedule")
		return
	}
	l.WithField("workers", wlen).Debug("start workers")
	if os.Getenv("JOBS_LEN") != "" {
		var err error
		wlen, err = strconv.Atoi(os.Getenv("JOBS_LEN"))
		if err != nil {
			CleanShutdownWithError(err, "failed to parse JOBS_LEN")
		}
	}
	l = l.WithField("jobLen", wlen)
	for i := 0; i < wlen; i++ {
		go orphanScheduleWorker(ctx, jobs, res)
	}
	for _, orphan := range orphans {
		l.WithField("block", orphan).Debug("indexing orphaned block")
		uintval := uint64(orphan)
		bj := BlockJob{
			ChainID:     chain,
			BlockNumber: uintval,
		}
		jobs <- bj
	}
	l.Debug("waiting for jobs to finish")
	close(jobs)
	for i := 0; i < wlen; i++ {
		l.Debug("waiting for job to finish: ", i)
		select {
		case err := <-res:
			if err != nil {
				CleanShutdownWithError(err, "failed to index orphaned block")
			}
		case <-ctx.Done():
			l.Debug("context done")
			return
		case <-time.After(time.Second * 20):
			l.Debug("timeout")
			return
		}
	}
}

// indexChainProcess is the main index process loop for each chain
func indexChainProcess(ctx context.Context, chain string) {
	l := log.WithFields(log.Fields{
		"action": "indexChainProcess",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	// if os.Getenv("RESCHEDULE_ORPHANS") == "true" && os.Getenv("RESCHEDULE_ASYNC") == "true" {
	// 	go rescheduleOrphans(ctx, chain)
	// } else if os.Getenv("RESCHEDULE_ORPHANS") == "true" {
	// 	rescheduleOrphans(ctx, chain)
	// }
	// l.Debug("start rescheduler")
	// go Rescheduler(ctx, chain)
	l.Debug("start indexer")
	for {
		l.Debug("start loop")
		time.Sleep(time.Second * 1)
		//rescheduleOrphans(ctx, chain)
		err := indexProcess(context.Background(), chain)
		if err != nil {
			CleanShutdownWithError(err, "failed to index chain")
		}
		l.Debug("end loop")
	}
}

func indexWorkerProcess(ctx context.Context, chain string) {
	l := log.WithFields(log.Fields{
		"action": "indexWorkerProcess",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	go indexBlockScheduler(ctx, chain)
	go ChainPersister(ctx, chain)
	go BackfillChainPersister(ctx, chain)
}

// indexChainProcessWorker accepts channel of chains to index and runs the index process
func indexChainProcessWorker(ctx context.Context, mode string, chains chan string, res chan string) {
	l := log.WithFields(log.Fields{
		"action": "indexChainProcessWorker",
		"mode":   mode,
	})
	l.Debug("start")
	defer l.Debug("end")
	for chain := range chains {
		switch mode {
		case "indexer":
			go indexChainProcess(ctx, chain)
		case "worker":
			go indexWorkerProcess(ctx, chain)
		default:
			go indexChainProcess(ctx, chain)
			go indexWorkerProcess(ctx, chain)
		}
		//go indexChainProcess(ctx, chain)
		// we do NOT send response on res channel to hold open
	}
}

// IndexChains indexes all txs in all blocks per chain
func IndexChains(ctx context.Context, Clients map[string]*ethclient.Client, mode string) {
	l := log.WithFields(log.Fields{
		"action": "indexChains",
	})
	l.Debug("start")
	defer l.Debug("end")
	procJobs := make(chan string, len(Clients))
	procRes := make(chan string, len(Clients))
	// create a new index process worker for each client
	for i := 0; i < len(Clients); i++ {
		go indexChainProcessWorker(ctx, mode, procJobs, procRes)
	}
	// push each chain to the index process worker
	for chain := range Clients {
		procJobs <- chain
	}
	// close process channels
	close(procJobs)
	// hold open indefinitely (we do not send back response)
	for i := 0; i < len(Clients); i++ {
		select {
		case <-procRes:
			// do nothing
		case <-ctx.Done():
			return
		}
	}
}

func CleanShutdownWithError(err error, message string) {
	log.WithError(err).Error(message)
	gdb, err := db.DB.DB()
	if err != nil {
		log.WithError(err).Error("failed to get db")
		os.Exit(1)
	}
	gdb.Close()
	os.Exit(1)
}
