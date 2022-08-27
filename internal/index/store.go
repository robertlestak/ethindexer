package index

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgconn"
	"github.com/robertlestak/ethindexer/internal/cache"
	"github.com/robertlestak/ethindexer/internal/db"
	"github.com/robertlestak/ethindexer/internal/work"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"
)

var (
	LastCheckedChains = make(map[string]uint64)
	LastCheckedMutex  sync.Mutex
)

// GetLastCheckedBlock returns the last checked block number for the defined chain
func (cs *ChainState) GetLastCheckedBlock() uint64 {
	l := log.WithFields(log.Fields{
		"chain":  cs.ChainID,
		"action": "GetLastCheckedBlock",
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
	return cs.LastCheckedBlock
}

// GetLastSoftCheckedBlock returns the last checked block number for the defined chain
// from either the database or queue, optimistically assuming that the queue will be worked
func (cs *ChainState) GetLastSoftCheckedBlock() uint64 {
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
	l = l.WithField("db_last_checked_block", dbl)
	l.Debug("db last checked block")
	cb, cberr := work.ChainBlocks(cs.ChainID)
	if cberr != nil {
		l.WithError(cberr).Error("error getting chain blocks")
		return dbl
	}
	if len(cb) == 0 {
		l.Debug("no chain blocks")
		return dbl
	}
	//l.Debug("chain blocks", cb)
	pv := uint64(cb[len(cb)-1])
	l = l.WithField("queue_last_checked_block", pv)
	l.Debug("queue last checked block")
	if pv > dbl {
		dbl = pv
	}
	return dbl
}

// UpdateLastCheckedBlock sets the last checked block number for the defined chain
func (cs *ChainState) UpdateLastCheckedBlock() {
	l := log.WithFields(log.Fields{
		"action": "UpdateLastCheckedBlock",
		"chain":  cs.ChainID,
	})
	l.Debug("start")
	defer l.Debug("end")
	if cs.ChainID == "" {
		l.Error("chain id is empty")
		return
	}
	res := db.DB.Save(cs)
	if res.Error != nil {
		l.WithError(res.Error).Error("error updating chain state")
	}
	cache.Flushable(cs.ChainID)
}

// IncrementLastCheckedBlock sets the last checked block number for the defined chain if it is larger than current
func (cs *ChainState) IncrementLastCheckedBlock() error {
	l := log.WithFields(log.Fields{
		"chain":  cs.ChainID,
		"action": "IncrementLastCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	if cs.ChainID == "" {
		return errors.New("chain id is empty")
	}
	var ts ChainState
	res := db.DB.FirstOrCreate(&ts, "chain_id = ?", cs.ChainID)
	if res.Error != nil {
		l.WithError(res.Error).Error("error getting chain state")
		return res.Error
	}
	if ts.LastCheckedBlock < cs.LastCheckedBlock {
		ts.LastCheckedBlock = cs.LastCheckedBlock
		res = db.DB.Save(&ts)
		if res.Error != nil {
			l.WithError(res.Error).Error("error saving chain state")
			return res.Error
		}
	}
	ferr := cache.Flushable(cs.ChainID)
	if ferr != nil {
		l.WithError(ferr).Error("error flushing chain state")
		return ferr
	}
	return nil
}

// IncrementLocalLastCheckedBlock sets the last checked block number for the defined chain if it is larger than current
func (cs *ChainState) IncrementLocalLastCheckedBlock() error {
	l := log.WithFields(log.Fields{
		"chain":  cs.ChainID,
		"action": "IncrementLocalLastCheckedBlock",
	})
	l.Debug("start")
	defer l.Debug("end")
	if cs.ChainID == "" {
		return errors.New("chain id is empty")
	}
	if _, ok := LastCheckedChains[cs.ChainID]; !ok {
		LastCheckedMutex.Lock()
		LastCheckedChains[cs.ChainID] = cs.LastCheckedBlock
		LastCheckedMutex.Unlock()
		return nil
	}
	if LastCheckedChains[cs.ChainID] < cs.LastCheckedBlock {
		LastCheckedMutex.Lock()
		LastCheckedChains[cs.ChainID] = cs.LastCheckedBlock
		LastCheckedMutex.Unlock()
	}
	return nil
}

func PersistLastChecked(chain string) error {
	l := log.WithFields(log.Fields{
		"action": "PersistLastChecked",
		"chain":  chain,
	})
	l.Debug("start")
	defer l.Debug("end")
	var found bool
	for k, v := range LastCheckedChains {
		if k != chain {
			continue
		}
		found = true
		cs := ChainState{
			ChainID:          k,
			LastCheckedBlock: v,
		}
		perr := cs.IncrementLastCheckedBlock()
		if perr != nil {
			l.WithError(perr).Error("error persisting chain state")
			return perr
		}
	}
	if !found {
		LastCheckedMutex.Lock()
		LastCheckedChains[chain] = 0
		LastCheckedMutex.Unlock()
	}
	return nil
}

func ChainPersister(ctx context.Context, chain string) error {
	l := log.WithFields(log.Fields{
		"action": "ChainPersister",
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
		err := PersistLastChecked(chain)
		if err != nil {
			l.WithError(err).Error("error persisting chain state")
		}
		time.Sleep(pdur)
	}
}

// Index indexes the transactions in the given block
func (t *Transaction) Index() error {
	l := log.WithFields(log.Fields{
		"action": "index",
		"tx":     t.Hash,
	})
	l.Debug("start")
	defer l.Debug("end")
	res := db.DB.Create(t)
	var perr *pgconn.PgError
	if res.Error != nil && errors.As(res.Error, &perr) && perr.Code == "23505" {
		l.WithError(res.Error).Info("error duplicate key")
		res := db.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "chain_hash"},
			},
			UpdateAll: true,
		}).Where("chain_hash = ?", t.ChainHash).Updates(t)
		if res.Error != nil {
			l.WithError(res.Error).Error("error updating transaction")
			return res.Error
		}
	} else if res.Error != nil {
		l.WithError(res.Error).Error("Error indexing transaction")
		return res.Error
	}
	cache.Flushable(t.ChainID)
	return nil
}
