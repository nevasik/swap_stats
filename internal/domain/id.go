package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// EventID = "<chain_id>:<tx_hash>:<log_index>"
func MakeEventID(chainID uint32, txHash string, logIndex uint32) string {
	return fmt.Sprintf("%d:%s:%d", chainID, strings.ToLower(txHash), logIndex)
}

type ParsedEventID struct {
	ChainID  uint32
	TxHash   string
	LogIndex uint32
}

func ParseEventID(id string) (ParsedEventID, error) {
	var out ParsedEventID
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid event_id format: %s", id)
	}

	chain, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return out, fmt.Errorf("invalid chain_id, err=%v", err)
	}

	logIdx, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return out, fmt.Errorf("invalid log_index, err=%v", err)
	}

	out.ChainID = uint32(chain)
	out.TxHash = strings.ToLower(parts[1])
	out.LogIndex = uint32(logIdx)

	return out, nil
}
