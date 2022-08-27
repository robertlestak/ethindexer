package index

import (
	"context"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	log "github.com/sirupsen/logrus"
)

func TokenIDFromTransaction(c *ethclient.Client, r *types.Receipt) (*big.Int, error) {
	l := log.WithFields(
		log.Fields{
			"action": "TokenIDFromTransaction",
		},
	)
	l.Debug("start")
	l.Debugf("GetTokenIDFromTransactionReceipt")
	for li, log := range r.Logs {
		l.Debugf("Log.Topics %d %v", li, log.Topics)
		for ti, topic := range log.Topics {
			l.Debugf("topic.Big %d %v", ti, topic.Big())
		}
	}
	if r.Logs == nil || len(r.Logs) == 0 {
		l.Debug("no logs")
		return nil, nil
	}
	// get first log
	ll := r.Logs[0]
	// get last topic
	if len(ll.Topics) == 0 {
		l.Debug("no topics")
		return nil, nil
	}
	lt := ll.Topics[len(ll.Topics)-1]
	l.Debugf("token ID %v", lt.Big())
	return lt.Big(), nil
}

// getEndBlock returns the block number to stop indexing at
// if env CONFIRM_BLOCKS is set, this will offset end block
func getEndBlock(ctx context.Context, c *ethclient.Client) (uint64, error) {
	l := log.WithFields(log.Fields{
		"action": "getEndBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	confirmBlocks := os.Getenv("CONFIRM_BLOCKS")
	if confirmBlocks == "" {
		confirmBlocks = "0"
	}
	confirmBlocksInt, err := strconv.Atoi(confirmBlocks)
	if err != nil {
		l.WithError(err).Error("failed to parse CONFIRM_BLOCKS")
		return 0, err
	}
	bn, berr := c.BlockNumber(ctx)
	if berr != nil {
		l.WithError(berr).Error("failed to get block number")
		return 0, berr
	}
	rb := bn - uint64(confirmBlocksInt)
	l.WithFields(log.Fields{
		"endBlock":      rb,
		"confirmBlocks": confirmBlocks,
	})
	return rb, nil
}
