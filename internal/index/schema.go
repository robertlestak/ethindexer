package index

import (
	"github.com/ethereum/go-ethereum/core/types"
	"gorm.io/gorm"
)

// ChainState contains the index state of the chain
type ChainState struct {
	gorm.Model
	ChainID          string `gorm:"uniqueIndex,not null"`
	LastCheckedBlock uint64
}

// Transaction represents a single indexed transaction
type Transaction struct {
	gorm.Model
	Time            int64
	ChainHash       string `gorm:"uniqueIndex,not null"`
	ChainID         string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr;index:idx_chain_id_contract_addr2;index:idx_chain_id_to_addr;index:idx_chain_id_from_addr"`
	Status          uint64 `gorm:"type:numeric"`
	BlockNumber     int64
	Hash            string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr"`
	ToAddr          string `gorm:"uniqueIndex:idx_chain_id_addrs;index:idx_chain_id_contract_addr2;index:idx_chain_id_to_addr"`
	FromAddr        string `gorm:"uniqueIndex:idx_chain_id_addrs;index:idx_chain_id_from_addr"`
	Value           int64
	Gas             int64
	GasPrice        int64
	ContractToAddr  string `gorm:"uniqueIndex:idx_chain_id_addrs;uniqueIndex:idx_chain_id_contract_to_addr;index:idx_chain_id_contract_addr2"`
	ContractValue   uint64 `gorm:"type:numeric"`
	ContractTokenID int64
	Data            []byte `json:"-" gorm:"-"`
}

// IndexJob contains a single transaction index job
type IndexJob struct {
	Chain       string
	BlockData   string
	Transaction *types.Transaction
}
