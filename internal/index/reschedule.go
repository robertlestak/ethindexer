package index

import (
	"context"
	"os"
	"time"

	"github.com/robertlestak/ethindexer/internal/db"
	"github.com/robertlestak/ethindexer/internal/work"
	log "github.com/sirupsen/logrus"
)

func intSliceContains(slice []uint64, item uint64) bool {
	for _, i := range slice {
		if i == item {
			return true
		}
	}
	return false
}

func rescheduleMissingBlocks(chain string) error {
	l := log.WithFields(
		log.Fields{
			"module": "index",
			"method": "rescheduleMissingBlocks",
			"chain":  chain,
		},
	)
	l.Debug("find missing blocks")
	var foundBlocks []uint64
	res := db.DB.Raw("select distinct block_number from transactions where chain_id = ? order by block_number asc", chain).Scan(&foundBlocks)
	if res.Error != nil {
		l.WithError(res.Error).Error("error getting blocks")
		return res.Error
	}
	if len(foundBlocks) == 0 {
		l.Debug("no blocks found")
		return nil
	}
	l.Debug("found blocks")
	headBlock := foundBlocks[len(foundBlocks)-1]
	l.WithField("headBlock", headBlock).Debug("head block")
	for i := uint64(0); i < headBlock; i++ {
		l.WithField("i", i).Debug("checking block")
		if !intSliceContains(foundBlocks, i) {
			l.WithField("missingBlock", i).Debug("missing block")
			if err := work.AddBlock(chain, i); err != nil {
				l.WithError(err).Error("error rescheduling block")
				return err
			}
		}
		i++
	}
	return nil
}

func Rescheduler(ctx context.Context, chain string) {
	l := log.WithFields(
		log.Fields{
			"module": "index",
			"method": "Rescheduler",
			"chain":  chain,
		},
	)
	l.Debug("starting rescheduler")
	for {
		rt := time.Minute * 10
		if os.Getenv("RESCHEDULE_DURATION") != "" {
			var err error
			rt, err = time.ParseDuration(os.Getenv("RESCHEDULE_DURATION"))
			if err != nil {
				l.WithError(err).Error("error parsing reschedule duration")
				return
			}
		}
		time.Sleep(rt)
		l.Debug("reschedule check")
		if os.Getenv("RESCHEDULE_ORPHANS") == "true" {
			rescheduleOrphans(ctx, chain)
		}
		/*
			if !work.Outstanding(chain) && os.Getenv("RESCHEDULE_MISSING_BLOCKS") == "true" {
				l.Debug("no outstanding work, try a reschedule")
				if rerr := rescheduleMissingBlocks(chain); rerr != nil {
					l.WithError(rerr).Errorf("error rescheduling blocks")
				}
			}
		*/
		l.Debug("rescheduler sleep")
	}
}
