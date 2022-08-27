package web3crypto

import (
	"context"
	"errors"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	Clients = make(map[string]*ethclient.Client)
)

func GetBlockchainClient(name string) (*ethclient.Client, error) {
	var c *ethclient.Client
	if c, ok := Clients[name]; ok {
		return c, nil
	}
	return c, errors.New("blockchain client not found")
}

func Init() error {
	var err error
	for _, e := range strings.Split(os.Getenv("ETH_ENDPOINTS"), ",") {
		e = strings.TrimSpace(e)
		ss := strings.Split(e, "=")
		if len(ss) != 2 {
			return errors.New("ETH_ENDPOINTS must be in the form of '<name>=<endpoint>'")
		}
		name := strings.TrimSpace(ss[0])
		endpoint := strings.TrimSpace(ss[1])
		log.Infof("connecting to ethereum: client=%s host=%s", name, endpoint)
		var c *ethclient.Client
		c, err = ethclient.Dial(endpoint)
		if err != nil {
			return err
		}
		Clients[name] = c
	}
	return nil
}

func GetTxByID(ctx context.Context, chain string, id string) (*types.Transaction, bool, error) {
	c, err := GetBlockchainClient(chain)
	if err != nil {
		return nil, false, err
	}
	hash := common.HexToHash(id)
	return c.TransactionByHash(ctx, hash)
}
