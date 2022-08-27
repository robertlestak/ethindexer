package index

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/robertlestak/ethindexer/internal/db"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ContractHolder is a holder of a contract
type ContractHolder struct {
	Address       string  `json:"address"`
	SenderAddress string  `json:"-"`
	Value         uint64  `json:"value"`
	TokenIDs      []int64 `json:"token_ids"`
}

// GetTransactionsByContract returns all transactions for a contract
func GetTransactionsByContract(contract string, chain string, tokenID int64) []Transaction {
	l := log.WithFields(log.Fields{
		"contract": contract,
		"chain":    chain,
		"func":     "GetTransactionsByContract",
		"tokenID":  tokenID,
	})
	l.Debug("Getting transactions by contract")
	var transactions []Transaction
	dbc := db.DBReader
	dbc = dbc.Order("time ASC")
	// COALESCE(status, 1) = 1 as we only recently started capturing status, need to backfill
	if tokenID == 0 {
		dbc.Where("chain_id = ? AND to_addr = ? AND contract_to_addr != '' AND COALESCE(status, 1) = 1", chain, contract).Find(&transactions)
	} else {
		dbc.Where("chain_id = ? AND to_addr = ? AND contract_token_id = ? AND contract_to_addr != '' AND COALESCE(status, 1) = 1", chain, contract, tokenID).Find(&transactions)
	}
	return transactions
}

// GetTransactionsByContractPager returns all transactions for a contract
func GetTransactionsByContractPager(contract string, chain string, tokenID int64, cached bool, page int, pageSize int) []Transaction {
	l := log.WithFields(log.Fields{
		"contract": contract,
		"chain":    chain,
		"func":     "GetTransactionsByContractPager",
		"tokenID":  tokenID,
		"cached":   cached,
		"page":     page,
		"pageSize": pageSize,
	})
	l.Debug("Getting transactions by contract")
	var transactions []Transaction

	// COALESCE(status, 1) = 1 as we only recently started capturing status, need to backfill
	if tokenID == 0 {
		db.DBReader.Scopes(db.Paginate(page, pageSize)).Where("chain_id = ? AND to_addr = ? AND contract_to_addr != '' AND COALESCE(status, 1) = 1", chain, contract).Find(&transactions)
	} else {
		db.DBReader.Scopes(db.Paginate(page, pageSize)).Where("chain_id = ? AND to_addr = ? AND contract_token_id = ? AND contract_to_addr != '' AND COALESCE(status, 1) = 1", chain, contract, tokenID).Find(&transactions)
	}
	return transactions
}

// GetTransactionsReceivedByAddress returns all transactions for a contract
func GetTransactionsReceivedByAddress(address string, chain string, page int, pageSize int, order string) []Transaction {
	l := log.WithFields(log.Fields{
		"address":  address,
		"chain":    chain,
		"func":     "GetTransactionsReceivedByAddress",
		"page":     page,
		"pageSize": pageSize,
		"order":    order,
	})
	l.Debug("Getting transactions by address")
	var transactions []Transaction
	if address == "" {
		l.Error("No address provided")
		return transactions
	}
	if chain == "" {
		l.Error("No chain provided")
		return transactions
	}
	var dbc *gorm.DB
	if pageSize <= 0 {
		dbc = db.DBReader
	} else {
		dbc = db.DBReader.Scopes(db.Paginate(page, pageSize))
	}
	if order == "asc" {
		dbc.Order("created_at ASC")
	} else {
		dbc.Order("created_at DESC")
	}
	// COALESCE(status, 1) = 1 as we only recently started capturing status, need to backfill
	dbc.Where("chain_id = ? AND to_addr = ? AND COALESCE(status, 1) = 1", chain, address).Find(&transactions)
	return transactions
}

// GetTransactionsSentFromAddress returns all transactions for a contract
func GetTransactionsSentFromAddress(address string, chain string, page int, pageSize int, order string) []Transaction {
	l := log.WithFields(log.Fields{
		"address":  address,
		"chain":    chain,
		"func":     "GetTransactionsSentFromAddress",
		"page":     page,
		"pageSize": pageSize,
		"order":    order,
	})
	l.Debug("Getting transactions by address")
	var transactions []Transaction
	if address == "" {
		l.Error("No address provided")
		return transactions
	}
	if chain == "" {
		l.Error("No chain provided")
		return transactions
	}
	var dbc *gorm.DB
	if pageSize <= 0 {
		dbc = db.DBReader
	} else {
		dbc = db.DBReader.Scopes(db.Paginate(page, pageSize))
	}
	if order == "asc" {
		dbc.Order("created_at ASC")
	} else {
		dbc.Order("created_at DESC")
	}
	// COALESCE(status, 1) = 1 as we only recently started capturing status, need to backfill
	dbc.Where("chain_id = ? AND from_addr = ? AND COALESCE(status, 1) = 1", chain, address).Find(&transactions)
	return transactions
}

// GetTransactionByID returns all transactions for an ID
func GetTransactionByID(chain string, txid string, cached bool) []Transaction {
	l := log.WithFields(log.Fields{
		"chain":  chain,
		"func":   "GetTransactionByID",
		"txid":   txid,
		"cached": cached,
	})
	l.Debug("Getting transactions by id")
	var transactions []Transaction
	if txid == "" {
		return transactions
	}
	// COALESCE(status, 1) = 1 as we only recently started capturing status, need to backfill
	db.DBReader.Where("chain_id = ? AND hash = ? AND COALESCE(status, 1) = 1", chain, txid).Find(&transactions)
	return transactions
}

func HandleGetTransactionByID(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetTransactionByID"})
	l.Debug("Handling get transaction by id")
	chain := r.URL.Query().Get("chain")
	txid := r.URL.Query().Get("txid")
	cachedstr := r.URL.Query().Get("cached")
	cached := true
	if cachedstr == "false" {
		cached = false
	}
	if txid == "" {
		l.Error("No txid provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if chain == "" {
		l.Error("No chain provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	transactions := GetTransactionByID(chain, txid, cached)
	jd, jerr := json.Marshal(transactions)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling transactions")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	/*
		if cerr := cache.CacheReqResponse(r, jd); cerr != nil {
			l.WithError(cerr).Error("Error caching token IDs")
		}
	*/
	w.Write(jd)
}

// HandleGetTransactionsByContract returns all transactions for a contract
func HandleGetTransactionsByContract(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetTransactionsByContract"})
	l.Debug("Handling get transactions by contract")
	contract := r.URL.Query().Get("contract")
	chain := r.URL.Query().Get("chain")
	tokenIDs := r.URL.Query().Get("tokenID")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	page := 1
	pageSize := 10
	var err error
	if pageStr != "" {
		page, err = strconv.Atoi(pageStr)
		if err != nil {
			l.WithError(err).Error("Error converting page to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if pageSizeStr != "" {
		pageSize, err = strconv.Atoi(pageSizeStr)
		if err != nil {
			l.WithError(err).Error("Error converting page size to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 10
	}

	var tokenID int64
	if tokenIDs != "" {
		var err error
		tokenID, err = strconv.ParseInt(tokenIDs, 10, 64)
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	transactions := GetTransactionsByContract(contract, chain, tokenID)
	jd, jerr := json.Marshal(transactions)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling transactions")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	/*
		if cerr := cache.CacheReqResponse(r, jd); cerr != nil {
			l.WithError(cerr).Error("Error caching token IDs")
		}
	*/
	w.Write(jd)
}

// HandleGetTransactionsReceivedByAddress returns all transactions received by an address
func HandleGetTransactionsReceivedByAddress(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetTransactionsReceivedByAddress"})
	l.Debug("Handling get transactions received by address")
	address := r.URL.Query().Get("address")
	chain := r.URL.Query().Get("chain")
	order := r.URL.Query().Get("order")
	if address == "" {
		l.Error("No address provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if chain == "" {
		l.Error("No chain provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	page := 1
	pageSize := 10
	var err error
	if pageStr != "" {
		page, err = strconv.Atoi(pageStr)
		if err != nil {
			l.WithError(err).Error("Error converting page to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if pageSizeStr != "" {
		pageSize, err = strconv.Atoi(pageSizeStr)
		if err != nil {
			l.WithError(err).Error("Error converting page size to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 10
	}
	transactions := GetTransactionsReceivedByAddress(address, chain, page, pageSize, order)
	jd, jerr := json.Marshal(transactions)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling transactions")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(jd)
}

// HandleGetTransactionsSentFromAddress returns all transactions received by an address
func HandleGetTransactionsSentFromAddress(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetTransactionsSentFromAddress"})
	l.Debug("Handling get transactions sent from address")
	address := r.URL.Query().Get("address")
	chain := r.URL.Query().Get("chain")
	order := r.URL.Query().Get("order")
	if address == "" {
		l.Error("No address provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if chain == "" {
		l.Error("No chain provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")
	page := 1
	pageSize := 10
	var err error
	if pageStr != "" {
		page, err = strconv.Atoi(pageStr)
		if err != nil {
			l.WithError(err).Error("Error converting page to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if pageSizeStr != "" {
		pageSize, err = strconv.Atoi(pageSizeStr)
		if err != nil {
			l.WithError(err).Error("Error converting page size to int")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 10
	}
	transactions := GetTransactionsSentFromAddress(address, chain, page, pageSize, order)
	jd, jerr := json.Marshal(transactions)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling transactions")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(jd)
}

func holderAddrs(holders []*ContractHolder) []string {
	addrs := make([]string, len(holders))
	for i, h := range holders {
		addrs[i] = h.Address
	}
	return addrs
}

func GetCurrentHoldersOfContract(contract string, chain string, tokenID int64) ([]*ContractHolder, error) {
	l := log.WithFields(log.Fields{"contract": contract, "chain": chain, "func": "GetCurrentHoldersOfContract"})
	l.Debug("Getting current holders of contract")
	var holders []*ContractHolder
	transactions := GetTransactionsByContract(contract, chain, tokenID)
	// loop through each transaction on this contract

	for _, transaction := range transactions {
		ll := l.WithFields(log.Fields{
			"transaction":    transaction.Hash,
			"fromAddr":       transaction.FromAddr,
			"toAddr":         transaction.ToAddr,
			"contractToAddr": transaction.ContractToAddr,
		})
		ll.Debug("Checking transaction")
		// has the holder been seen before?
		var hFound bool
		// loop through each holder on this contract
		ll.Debugf("Holders: %+v", holderAddrs(holders))
		for _, h := range holders {
			// if the holder is already in the list, and the transaction is sent BY the holder
			// then they are going to lose the token
			ll.WithFields(log.Fields{
				"holder":         h.Address,
				"contractToAddr": transaction.ContractToAddr,
				"fromAddr":       transaction.FromAddr,
			}).Debugf("Checking holder")
			if strings.EqualFold(h.Address, transaction.ContractToAddr) {
				ll.Debugf("Holder found as the recipient: %s", h.Address)
				// we found the holder
				hFound = true
				// add the amount to the holder's balance
				h.Value += uint64(transaction.Value)
				// if the transaction has a token ID, then it is a token transaction
				// the value above will be 0, and the actual valuation is the token
				if transaction.ContractTokenID != 0 {
					h.TokenIDs = append(h.TokenIDs, transaction.ContractTokenID)
				}
			} else if strings.EqualFold(h.Address, transaction.FromAddr) {
				ll.Debugf("Holder found as the sender: %s", h.Address)
				// we found the holder, but we need to create a new one for the recipient
				hFound = false
				// subtract the amount from the holder's balance
				h.Value -= uint64(transaction.Value)
				// if the transaction has a token ID, then it is a token transaction
				// the value above will be 0, and the actual valuation is the token
				if transaction.ContractTokenID != 0 {
					var ntids []int64
					// loop through the tokens the holder has
					for _, tid := range h.TokenIDs {
						// and remove the current token ID from the list
						if tid != transaction.ContractTokenID {
							ntids = append(ntids, tid)
						}
					}
					// set the holder's token IDs to the new list
					h.TokenIDs = ntids
				}
				// if the holder is the transaction recipient
			}
		}
		// if the holder was not found, and they are not sending the transaction
		// then add them to the list
		// && strings.EqualFold(transaction.ContractToAddr, transaction.FromAddr)
		if !hFound && !strings.EqualFold(transaction.ContractToAddr, transaction.FromAddr) {
			ll.Debugf("Holder not found: %s", transaction.ContractToAddr)
			var h ContractHolder
			// set the holder's address
			h.Address = transaction.ContractToAddr
			// set the sender address so we can check if they are sending the token
			h.SenderAddress = transaction.FromAddr
			// set the holder's balance
			h.Value = uint64(transaction.Value)
			// if the transaction has a token ID, then it is a token transaction
			// the value above will be 0, and the actual valuation is the token
			if transaction.ContractTokenID != 0 {
				// set the holder's token IDs
				h.TokenIDs = []int64{transaction.ContractTokenID}
			}
			// add the holder to the list
			holders = append(holders, &h)
		}
	}
	// create a clean list of holders
	var nh []*ContractHolder
	// loop over the holders
	for _, hv := range holders {
		// if the holder has no tokens and no value, then they are not a holder
		if hv.Value == 0 && len(hv.TokenIDs) == 0 {
			continue
		}
		// add the holder to the clean list
		nh = append(nh, hv)
	}
	return nh, nil
}

func HandleGetCurrentHoldersOfContract(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetCurrentHoldersOfContract"})
	l.Debug("Handling get holders of contract")
	contract := r.URL.Query().Get("contract")
	chain := r.URL.Query().Get("chain")
	tokenIDs := r.URL.Query().Get("tokenID")
	var tokenID int64
	if tokenIDs != "" {
		var err error
		tokenID, err = strconv.ParseInt(tokenIDs, 10, 64)
		if err != nil {
			l.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if contract == "" || chain == "" {
		l.Error("No contract or chain provided")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	holders, err := GetCurrentHoldersOfContract(contract, chain, tokenID)
	if err != nil {
		l.WithError(err).Error("Error getting holders of contract")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	jd, jerr := json.Marshal(holders)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling holders")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	/*
		if cerr := cache.CacheReqResponse(r, jd); cerr != nil {
			l.WithError(cerr).Error("Error caching token IDs")
		}
	*/
	w.Write(jd)
}

func uniqueIn64Slice(s []int64) []int64 {
	m := make(map[int64]bool)
	var u []int64
	for _, v := range s {
		if _, found := m[v]; !found {
			u = append(u, v)
			m[v] = true
		}
	}
	return u
}

func GetTokenIDsForContract(contract string, chain string) []int64 {
	l := log.WithFields(log.Fields{"contract": contract, "chain": chain, "func": "GetTokenIDsForContract"})
	l.Debug("Getting token IDs for contract")
	var tokenIDs []int64
	if contract == "" || chain == "" {
		l.Error("Contract or chain empty")
		return tokenIDs
	}
	transactions := GetTransactionsByContract(contract, chain, 0)
	for _, transaction := range transactions {
		if transaction.ContractTokenID != 0 {
			tokenIDs = append(tokenIDs, transaction.ContractTokenID)
		}
	}
	tokenIDs = uniqueIn64Slice(tokenIDs)
	l.WithField("tokenIDs", tokenIDs).Debug("Got token IDs for contract")
	return tokenIDs
}

func HandleGetTokenIDsByContract(w http.ResponseWriter, r *http.Request) {
	l := log.WithFields(log.Fields{"func": "HandleGetTokenIDsByContract"})
	l.Debug("Handling get token IDs by contract")
	contract := r.URL.Query().Get("contract")
	chain := r.URL.Query().Get("chain")
	if contract == "" || chain == "" {
		l.Error("Missing contract or chain")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	tokenIDs := GetTokenIDsForContract(contract, chain)
	jd, jerr := json.Marshal(tokenIDs)
	if jerr != nil {
		l.WithError(jerr).Error("Error marshalling token IDs")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	/*
		if cerr := cache.CacheReqResponse(r, jd); cerr != nil {
			l.WithError(cerr).Error("Error caching token IDs")
		}
	*/
	w.Write(jd)
}
