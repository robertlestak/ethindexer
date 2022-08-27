package work

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/go-redis/redis"
	log "github.com/sirupsen/logrus"
)

var (
	Client *redis.Client
)

func Init() error {
	l := log.WithFields(log.Fields{
		"package": "work",
	})
	l.Info("Initializing redis client")
	Client = redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password:    "", // no password set
		DB:          0,  // use default DB
		DialTimeout: 30 * time.Second,
		ReadTimeout: 30 * time.Second,
	})
	cmd := Client.Ping()
	if cmd.Err() != nil {
		l.Error("Failed to connect to redis")
		return cmd.Err()
	}
	l.Info("Connected to redis")
	return nil
}

func AddTransaction(chain string, blockNumber uint64, blockTime uint64, tx types.Transaction, side string) error {
	blockData := fmt.Sprintf("%s_%s", strconv.FormatUint(blockNumber, 10), strconv.FormatUint(blockTime, 10))
	if side == "" {
		side = "right"
	}
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"block":   blockData,
		"side":    side,
		"tx":      tx.Hash().String(),
	})
	l.Debug("Adding tx to redis")
	jd, jerr := tx.MarshalJSON()
	if jerr != nil {
		l.Error("Failed to marshal tx")
		return jerr
	}
	var cmd *redis.IntCmd
	if side == "right" {
		cmd = Client.RPush(chain+":txs", blockData+":"+string(jd))
	} else {
		cmd = Client.LPush(chain+":txs", blockData+":"+string(jd))
	}
	if cmd.Err() != nil {
		l.Error("Failed to add tx to redis")
		return cmd.Err()
	}
	cmd = Client.SAdd(chain+":block:"+strconv.FormatUint(blockNumber, 10), tx.Hash().String())
	if cmd.Err() != nil {
		l.Error("Failed to add tx to redis")
		return cmd.Err()
	}
	l.Debug("Added tx to redis")
	return nil
}

func AddBlockTransactions(chain string, block *types.Block, side string) error {
	l := log.WithFields(log.Fields{
		"package": "work",
		"action":  "addBlockTransactions",
		"block":   block.Number().String(),
		"chain":   chain,
		"side":    side,
	})
	l.Debug("Adding block transactions to redis")
	txs := block.Transactions()
	l.Debugf("Adding tx to redis (%d)", len(txs))
	for _, tx := range txs {
		l.WithField("tx", tx.Hash().String()).Debug("Adding tx to redis")
		aerr := AddTransaction(chain, block.NumberU64(), block.Time(), *tx, side)
		if aerr != nil {
			l.Error("Failed to add tx to redis")
			return aerr
		}
	}
	l.Debug("Added tx to redis")
	return nil
}

func GetNextTx(chain string, order string) (*types.Transaction, string, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"action":  "getNextTx",
		"chain":   chain,
		"order":   order,
	})
	tx := &types.Transaction{}
	l.Debug("Getting tx from redis")
	var cmd *redis.StringCmd
	if order == "" {
		order = "asc"
	}
	if order == "asc" {
		cmd = Client.LPop(chain + ":txs")
	} else {
		cmd = Client.RPop(chain + ":txs")
	}
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to get tx from redis")
		return tx, "", cmd.Err()
	} else if cmd.Err() == redis.Nil {
		l.Debug("No txs in redis")
		return nil, "", nil
	}
	l.Debug("Got tx from redis")
	var blockData string
	var txs string
	ss := strings.Split(cmd.Val(), ":")
	blockData = ss[0]
	txs = strings.Join(ss[1:], ":")
	err := tx.UnmarshalJSON([]byte(txs))
	if err != nil {
		l.Error("Failed to unmarshal tx")
		return tx, "", err
	}
	return tx, blockData, nil
}

func GetNextBlock(chain string) (uint64, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"action":  "GetNextBlock",
		"chain":   chain,
	})
	l.Debug("Getting block from redis")
	cmd := Client.LPop(chain + ":blocks")
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to get block from redis")
		return 0, cmd.Err()
	} else if cmd.Err() == redis.Nil {
		l.Debug("No blocks in redis")
		return 0, nil
	}
	block := cmd.Val()
	l.WithField("block", block).Debug("Got block from redis")
	return strconv.ParseUint(block, 10, 64)
}

func AddBlock(chain string, blockNumber uint64) error {
	l := log.WithFields(log.Fields{
		"package": "work",
		"action":  "addBlock",
		"chain":   chain,
		"block":   blockNumber,
	})
	l.Debug("Adding block to redis")
	cmd := Client.RPush(chain+":blocks", strconv.FormatUint(blockNumber, 10))
	if cmd.Err() != nil {
		l.Error("Failed to add block to redis")
		return cmd.Err()
	}
	l.Debug("Added block to redis")
	return nil
}

func BlockIndexComplete(chain string, block string) (bool, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"block":   block,
	})
	l.Debug("Check if block index is complete")
	cmd := Client.Scan(0, chain+":block:"+block, 1)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to get block index keys")
		return false, cmd.Err()
	}
	blocks, _ := cmd.Val()
	if len(blocks) == 0 {
		l.Debug("Block index is complete")
		return true, nil
	}
	l.Debug("Block index is not complete.")
	return false, nil
}

func IndexComplete(chain string, block string, txid string) (bool, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"block":   block,
		"txid":    txid,
	})
	l.Debug("Indexing complete")
	cmd := Client.SRem(chain+":block:"+block, txid)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to remove tx from redis")
		return false, cmd.Err()
	}
	l.Debug("Removed tx from redis")
	c, cerr := BlockIndexComplete(chain, block)
	if cerr != nil {
		l.Error("Failed to check if block index is complete")
		return c, cerr
	}
	return c, nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func int64InSlice(a int64, list []int64) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func PendingTxs(chain string) ([]string, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "pendingTxs",
	})
	var txs []string
	l.Debug("Getting pending txs")
	cmd := Client.LRange(chain+":txs", 0, -1)
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to get block index keys")
		return txs, cmd.Err()
	} else if cmd.Err() == redis.Nil {
		l.Debug("No pending txs")
		return txs, nil
	}
	txs = cmd.Val()
	l.Debug("Got pending txs. len=%d", len(txs))
	return txs, nil
}

func PendingTxsCount(chain string) (int64, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "PendingTxsCount",
	})
	var count int64
	l.Debug("Getting pending txs count")
	cmd := Client.LLen(chain + ":txs")
	if cmd.Err() != nil && cmd.Err() != redis.Nil {
		l.Error("Failed to get block index keys")
		return count, cmd.Err()
	} else if cmd.Err() == redis.Nil {
		l.Debug("No pending txs")
		return count, nil
	}
	count = cmd.Val()
	l.Debug("Got pending txs. len=%d", count)
	return count, nil
}

func ChainBlocks(chain string) ([]int64, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "ChainBlocks",
	})
	l.Debug("Getting blocks")
	var blocks []int64
	var cursor uint64
	var initCursor bool = true
	for cursor > 0 || initCursor {
		scmd := Client.Scan(cursor, chain+":block:*", 1000)
		if scmd.Err() != nil && scmd.Err() != redis.Nil {
			l.Error("Failed to get block index keys")
			return nil, scmd.Err()
		}
		var lblocks []string
		lblocks, cursor = scmd.Val()
		for _, b := range lblocks {
			bs := strings.Split(b, ":")
			block := bs[2]
			iv, ierr := strconv.ParseInt(block, 10, 64)
			if ierr != nil {
				l.Error("Failed to parse block number")
				return nil, ierr
			}
			if !int64InSlice(iv, blocks) {
				blocks = append(blocks, iv)
			}
		}
		initCursor = false
	}
	ssr := Client.LRange(chain+":blocks", 0, -1)
	if ssr.Err() != nil && ssr.Err() != redis.Nil {
		l.Error("Failed to get block index keys")
		return nil, ssr.Err()
	}
	for _, v := range ssr.Val() {
		iv, ierr := strconv.ParseInt(v, 10, 64)
		if ierr != nil {
			l.Error("Failed to parse block number")
			return nil, ierr
		}
		if !int64InSlice(iv, blocks) {
			blocks = append(blocks, iv)
		}
	}
	l.Debugf("Got %d pending blocks", len(blocks))
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i] < blocks[j]
	})
	return blocks, nil
}

func BlockInQueue(chain string, block uint64) (bool, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "BlockInQueue",
		"block":   block,
	})
	l.Debug("Checking if block in queue")
	bs, berr := ChainBlocks(chain)
	if berr != nil {
		l.Error("Failed to get blocks")
		return false, berr
	}
	if len(bs) == 0 {
		l.Debug("No blocks")
		return false, nil
	}
	ublock := uint64(bs[len(bs)-1])
	if block < ublock {
		l.Debug("block lower than last block, assuming we have passed it")
		return true, nil
	}
	l.Debug("block not in queue")
	return false, nil
}

func RemoveBlock(chain string, block uint64) error {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "RemoveBlock",
		"block":   block,
	})
	l.Debug("Removing block")
	defer l.Debug("Removed block")
	cmd := Client.LRem(chain+":blocks", 0, strconv.FormatUint(block, 10))
	if cmd.Err() != nil {
		l.Error("Failed to remove block", cmd.Err())
		//return cmd.Err()
	}
	cmd = Client.Del(chain + ":block:" + strconv.FormatUint(block, 10))
	if cmd.Err() != nil {
		l.Error("Failed to remove block", cmd.Err())
		//return cmd.Err()
	}
	return nil
}

func OrphanedBlocks(chain string) ([]int64, error) {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
	})
	l.Debug("Getting orphaned blocks")
	var foundTxs []string
	maxTxs := 1000
	var cursor int
	moreTxs := true
	for moreTxs {
		l.WithFields(log.Fields{
			"cursor": cursor,
			"maxTxs": maxTxs,
		}).Debug("Getting pending chain txs")
		cmd := Client.LRange(chain+":txs", int64(cursor), int64(cursor+maxTxs))
		if cmd.Err() != nil && cmd.Err() != redis.Nil {
			l.Error("Failed to get block index keys")
			return nil, cmd.Err()
		}
		txs := cmd.Val()
		if len(txs) == 0 {
			moreTxs = false
			continue
		}
		foundTxs = append(foundTxs, txs...)
		cursor += maxTxs
	}
	txs := foundTxs
	l.WithField("txs", len(txs)).Debug("Got pending txs")
	var foundBlocks []int64
	for _, tx := range txs {
		ss := strings.Split(tx, ":")
		blockData := ss[0]
		bb := strings.Split(blockData, "_")
		blockNumber := bb[0]
		blockNumberInt, berr := strconv.ParseInt(blockNumber, 10, 64)
		if berr != nil {
			l.Error("Failed to parse block number")
			return nil, berr
		}
		foundBlocks = append(foundBlocks, blockNumberInt)
	}
	l.Debugf("Got %d outstanding transactions", len(txs))
	blocks, err := ChainBlocks(chain)
	if err != nil {
		l.Error("Failed to get blocks")
		return nil, err
	}
	l.Debugf("Got %d blocks", len(blocks))
	var orphans []int64
	for _, b := range blocks {
		if len(txs) == 0 {
			orphans = append(orphans, b)
			continue
		}
		if !int64InSlice(b, foundBlocks) {
			orphans = append(orphans, b)
		}
	}
	l.Debugf("Got %d orphaned blocks", len(orphans))
	return orphans, nil
}

func BacklogWait(chain string) error {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "backlogWait",
	})
	l.Debug("Waiting for backlog")
	maxPending := 0
	var err error
	if os.Getenv("MAX_PENDING_TXS") != "" {
		maxPending, err = strconv.Atoi(os.Getenv("MAX_PENDING_TXS"))
		if err != nil {
			l.WithError(err).Error("failed to parse MAX_PENDING_TXS")
			return err
		}
	}
	if maxPending == 0 {
		l.Debug("MAX_PENDING_TXS not set.  Not waiting for backlog")
		return nil
	}
	ptxs, perr := PendingTxsCount(chain)
	if perr != nil {
		l.WithError(perr).Error("failed to get pending txs")
		return perr
	}
	for ptxs > int64(maxPending) {
		l.WithFields(
			log.Fields{
				"pendingTxs": ptxs,
				"maxPending": maxPending,
			},
		).Debug("waiting for pending txs to clear")
		time.Sleep(time.Millisecond)
		ptxs, perr = PendingTxsCount(chain)
		if perr != nil {
			l.WithError(perr).Error("failed to get pending txs")
			return perr
		}
	}
	return nil
}

func Outstanding(chain string) bool {
	l := log.WithFields(log.Fields{
		"package": "work",
		"chain":   chain,
		"action":  "outstanding",
	})
	l.Debug("Checking for outstanding txs")
	var cursor uint64
	var initCursor bool = true
	for cursor > 0 || initCursor {
		scmd := Client.Scan(cursor, chain+":blocks", 1000)
		if scmd.Err() != nil && scmd.Err() != redis.Nil {
			l.Error("Failed to get block keys")
			return false
		}
		var lblocks []string
		lblocks, cursor = scmd.Val()
		if len(lblocks) > 0 {
			l.Debug("Found outstanding txs")
			return true
		}
		initCursor = false
	}
	l.Debug("No outstanding txs")
	return false
}
