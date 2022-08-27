package index

import (
	"context"
	"errors"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/robertlestak/ethindexer/internal/db"
	"github.com/robertlestak/ethindexer/internal/web3crypto"
	"github.com/robertlestak/ethindexer/internal/work"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var (
	BackfillChains           = make(map[string]uint64)
	BackfillLastCheckedMutex sync.Mutex
)

// ChainStateBackfill contains the index state of the chain
type ChainStateBackfill struct {
	gorm.Model
	ChainID          string `gorm:"uniqueIndex,not null"`
	EndBlock         uint64
	LastCheckedBlock uint64
}

// GetLastCheckedBlock returns the last checked block number for the defined chain
func (b *ChainStateBackfill) GetLastCheckedBlock() uint64 {
	l := log.WithFields(log.Fields{
		"chain":  b.ChainID,
		"action": "GetLastCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	res := db.DB.FirstOrCreate(b, "chain_id = ?", b.ChainID)
	if res.Error != nil {
		l.WithError(res.Error).Error("failed to get chain state")
		return 0
	}
	return b.LastCheckedBlock
}

// GetLastSoftCheckedBlock returns the last checked block number for the defined chain
// from either the database or queue, optimistically assuming that the queue will be worked
func (cs *ChainStateBackfill) GetLastSoftCheckedBlock() uint64 {
	l := log.WithFields(log.Fields{
		"chain":  cs.ChainID,
		"action": "GetLastSoftCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	if cs.ChainID == "" {
		return 0
	}
	res := db.DB.FirstOrCreate(cs, "chain_id = ?", cs.ChainID)
	if res.Error != nil {
		l.WithError(res.Error).Error("error getting chain state")
		return 0
	}
	dbl := cs.LastCheckedBlock
	cb, cberr := work.ChainBlocks(cs.ChainID)
	if cberr != nil {
		l.WithError(cberr).Error("error getting chain blocks")
		return dbl
	}
	pv := uint64(cb[len(cb)-1])
	if pv > dbl {
		dbl = pv
	}
	return dbl
}

// UpdateLastCheckedBlock sets the last checked block number for the defined chain
func (b *ChainStateBackfill) UpdateLastCheckedBlock() error {
	l := log.WithFields(log.Fields{
		"chain":  b.ChainID,
		"action": "UpdateLastCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	var ts ChainStateBackfill
	res := db.DB.FirstOrCreate(&ts, "chain_id = ?", b.ChainID)
	if res.Error != nil {
		l.WithError(res.Error).Error("failed to get chain state")
		return res.Error
	}
	if b.LastCheckedBlock > ts.EndBlock && b.LastCheckedBlock < ts.LastCheckedBlock {
		ts.LastCheckedBlock = b.LastCheckedBlock
		res = db.DB.Save(&ts)
		if res.Error != nil {
			l.WithError(res.Error).Error("failed to update chain state")
			return res.Error
		}
	}
	return nil
}

// UpdateLocalLastCheckedBlock sets the last checked block number for the defined chain if it is larger than current
func (cs *ChainStateBackfill) UpdateLocalLastCheckedBlock() error {
	l := log.WithFields(log.Fields{
		"chain":  cs.ChainID,
		"action": "UpdateLocalLastCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	if cs.ChainID == "" {
		return errors.New("chain id is empty")
	}
	BackfillLastCheckedMutex.Lock()
	BackfillChains[cs.ChainID] = cs.LastCheckedBlock
	BackfillLastCheckedMutex.Unlock()
	return nil
}

func PersistLastCheckedBackfill(chain string) error {
	l := log.WithFields(log.Fields{
		"action": "PersistLastCheckedBackfill",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var found bool
	for k, v := range BackfillChains {
		if k != chain {
			continue
		}
		found = true
		cs := ChainStateBackfill{
			ChainID:          k,
			LastCheckedBlock: v,
		}
		perr := cs.UpdateLastCheckedBlock()
		if perr != nil {
			l.WithError(perr).Error("error persisting chain state")
			return perr
		}
	}
	if !found {
		BackfillLastCheckedMutex.Lock()
		BackfillChains[chain] = 0
		BackfillLastCheckedMutex.Unlock()
	}
	return nil
}

func BackfillChainPersister(ctx context.Context, chain string) error {
	l := log.WithFields(log.Fields{
		"action": "BackfillChainPersister",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	pdur := time.Minute
	var err error
	if os.Getenv("PERSIST_DURATION") != "" {
		pdur, err = time.ParseDuration(os.Getenv("PERSIST_DURATION"))
		if err != nil {
			l.WithError(err).Error("error parsing persist duration")
			return err
		}
	}
	for {
		err := PersistLastCheckedBackfill(chain)
		if err != nil {
			l.WithError(err).Error("error persisting chain state")
		}
		time.Sleep(pdur)
	}
}

func (b *ChainStateBackfill) Get() *ChainStateBackfill {
	l := log.WithFields(log.Fields{
		"chain":  b.ChainID,
		"action": "ChainStateBackfill.Get",
	})
	l.Debug("start")
	defer l.Debug("end")
	res := db.DB.FirstOrCreate(b, "chain_id = ?", b.ChainID)
	if res.Error != nil {
		l.WithError(res.Error).Error("failed to get chain state")
		return nil
	}
	return b
}

func backfillBlockByNumber(ctx context.Context, c *ethclient.Client, chain string, blockNumber uint64) (types.Transactions, error) {
	l := log.WithFields(log.Fields{
		"action": "backfillBlockByNumber",
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
		return nil, err
	}
	txs := block.Transactions()
	if len(txs) == 0 {
		l.Debug("no transactions to index")
		cs := &ChainStateBackfill{
			ChainID:          chain,
			LastCheckedBlock: blockNumber,
		}
		if rerr := work.RemoveBlock(chain, blockNumber); rerr != nil {
			l.WithError(rerr).Error("failed to remove block")
			return txs, rerr
		}
		cs.UpdateLocalLastCheckedBlock()
		return txs, nil
	}
	l.Debugf("Adding tx to redis (%d)", len(txs))
	for _, tx := range txs {
		if err := indexTransaction(ctx, c, block.NumberU64(), int64(block.Time()), *tx, chain); err != nil {
			l.WithError(err).Error("failed to index transaction")
			return txs, err
		}
	}
	cs := &ChainStateBackfill{
		ChainID:          chain,
		LastCheckedBlock: blockNumber,
	}
	cs.UpdateLocalLastCheckedBlock()
	l.Debug("end")
	return txs, nil
}

// indexProcess is the main index process for each chain
func backfillProcess(ctx context.Context, chain string) error {
	l := log.WithFields(log.Fields{
		"action": "backfillProcess",
		"chain":  chain,
	})
	l.Debug("start")
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
	cs := &ChainStateBackfill{
		ChainID: chain,
	}
	cs.Get()
	if cs == nil {
		l.Error("failed to get chain state")
		return err
	}
	l.WithFields(
		log.Fields{
			"lastCheckedBlock": cs.LastCheckedBlock,
			"searchEndBlock":   cs.EndBlock,
			"chainEndBlock":    endBlock,
		},
	).Debug("start indexing")
	var retries int
	blockStepDelay := time.Millisecond * 10
	if os.Getenv("BACKFILL_BLOCK_STEP_DELAY") != "" {
		var err error
		blockStepDelay, err = time.ParseDuration(os.Getenv("BACKFILL_BLOCK_STEP_DELAY"))
		if err != nil {
			l.WithError(err).Error("failed to parse BACKFILL_BLOCK_STEP_DELAY")
			return err
		}
	}
	for cs.LastCheckedBlock > cs.EndBlock {
		cs.LastCheckedBlock--
		txs, err := backfillBlockByNumber(ctx, c, chain, cs.LastCheckedBlock)
		if err != nil {
			l.WithError(err).Error("failed to index block")
			retries++
			if retries > 10 {
				l.WithField("retries", retries).Error("failed to index block")
				cs.LastCheckedBlock--
				retries = 0
			}
			continue
		} else {
			retries = 0
		}
		if len(txs) == 0 {
			continue
		} else {
			time.Sleep(blockStepDelay)
		}
	}
	l.Debug("end")
	return nil
}

// backfillChainProcess is the main index process loop for each chain
func backfillChainProcess(ctx context.Context, chain string) {
	l := log.WithFields(log.Fields{
		"action": "backfillChainProcess",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	for {
		l.Debug("start loop")
		err := backfillProcess(context.Background(), chain)
		if err != nil {
			l.WithError(err).Errorf("failed to index chain")
		}
		l.Debug("end loop")
		time.Sleep(time.Millisecond * 500)
	}
}

// backfillChainProcessWorker accepts channel of chains to index and runs the index process
func backfillChainProcessWorker(ctx context.Context, mode string, chains chan string, res chan string) {
	l := log.WithFields(log.Fields{
		"action": "backfillChainProcessWorker",
		"mode":   mode,
	})
	l.Debug("start")
	defer l.Debug("end")
	for chain := range chains {
		go backfillChainProcess(ctx, chain)
		//go indexChainProcess(ctx, chain)
		// we do NOT send response on res channel to hold open
	}
}

// BackFillChains indexes all txs in all blocks per chain
func BackFillChains(ctx context.Context, Clients map[string]*ethclient.Client, mode string) {
	l := log.WithFields(log.Fields{
		"action": "BackFillChains",
	})
	l.Debug("start")
	defer l.Debug("end")
	procJobs := make(chan string, len(Clients))
	procRes := make(chan string, len(Clients))
	// create a new index process worker for each client
	for i := 0; i < len(Clients); i++ {
		go backfillChainProcessWorker(ctx, mode, procJobs, procRes)
	}
	// push each chain to the index process worker
	for chain := range Clients {
		procJobs <- chain
	}
	// close process channels
	close(procJobs)
	// hold open indefinitely (we do not send back response)
	for i := 0; i < len(Clients); i++ {
		<-procRes
	}
}
