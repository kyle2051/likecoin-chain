package txs

import (
	"strconv"

	"github.com/likecoin/likechain/abci/account"
	"github.com/likecoin/likechain/abci/context"
	"github.com/likecoin/likechain/abci/response"
	"github.com/likecoin/likechain/abci/state/deposit"
	"github.com/likecoin/likechain/abci/txstatus"
	"github.com/likecoin/likechain/abci/types"

	cmn "github.com/tendermint/tendermint/libs/common"
)

// DepositTransaction represents a Deposit transaction
type DepositTransaction struct {
	Proposer types.Identifier
	Proposal deposit.Proposal
	Nonce    uint64
	Sig      DepositSignature
}

// ValidateFormat checks if a transaction has invalid format, e.g. nil fields, negative transfer amounts
func (tx *DepositTransaction) ValidateFormat() bool {
	if tx.Proposer == nil || tx.Sig == nil {
		return false
	}
	if len(tx.Proposal.Inputs) == 0 {
		return false
	}
	for _, input := range tx.Proposal.Inputs {
		if input.Value.Int == nil || !input.Value.IsWithinRange() {
			return false
		}
	}
	return true
}

func (tx *DepositTransaction) checkTx(state context.IImmutableState) (
	r response.R, senderID *types.LikeChainID,
) {
	if !tx.ValidateFormat() {
		logTx(tx).Info(response.DepositInvalidFormat.Info)
		return response.DepositInvalidFormat, nil
	}

	senderID = account.IdentifierToLikeChainID(state, tx.Proposer)
	if senderID == nil {
		logTx(tx).Info(response.DepositSenderNotRegistered.Info)
		return response.DepositSenderNotRegistered, nil
	}

	addr, err := tx.Sig.RecoverAddress(tx)
	if err != nil || !account.IsLikeChainIDHasAddress(state, senderID, addr) {
		logTx(tx).
			WithField("recovered_addr", addr).
			WithError(err).
			Info(response.DepositInvalidSignature.Info)
		return response.DepositInvalidSignature, senderID
	}

	nextNonce := account.FetchNextNonce(state, senderID)
	if tx.Nonce > nextNonce {
		logTx(tx).Info(response.DepositInvalidNonce.Info)
		return response.DepositInvalidNonce, senderID
	} else if tx.Nonce < nextNonce {
		logTx(tx).Info(response.DepositDuplicated.Info)
		return response.DepositDuplicated, senderID
	}

	return deposit.CheckDepositProposal(state, tx.Proposal, senderID), senderID
}

// CheckTx checks the transaction to see if it should be executed
func (tx *DepositTransaction) CheckTx(state context.IImmutableState) response.R {
	r, _ := tx.checkTx(state)
	return r
}

// DeliverTx checks the transaction to see if it should be executed
func (tx *DepositTransaction) DeliverTx(state context.IMutableState, txHash []byte) response.R {
	checkTxRes, senderID := tx.checkTx(state)
	if checkTxRes.Code != 0 {
		switch checkTxRes.Code {
		case response.DepositAlreadyExecuted.Code:
			fallthrough
		case response.DepositDoubleApproval.Code:
			fallthrough
		case response.DepositNotApprover.Code:
			account.IncrementNextNonce(state, senderID)
		}
		return checkTxRes
	}

	account.IncrementNextNonce(state, senderID)
	weight := deposit.CreateDepositProposal(state, txHash, tx.Proposal, senderID)
	txStatus := txstatus.TxStatusPending

	height := state.GetHeight() + 1
	tags := []cmn.KVPair{
		{
			Key:   []byte("deposit.height"),
			Value: []byte(strconv.FormatInt(height, 10)),
		},
	}

	weightSum := deposit.GetDepositApproversWeightSum(state)
	if weight*3 > weightSum*2 {
		deposit.ExecuteDepositProposal(state, txHash)
		txStatus = txstatus.TxStatusSuccess
		tags = append(tags, cmn.KVPair{
			Key:   []byte("deposit_execution.height"),
			Value: []byte(strconv.FormatInt(height, 10)),
		})
	}

	return response.Success.Merge(response.R{
		Tags:   tags,
		Status: txStatus,
	})
}

// DepositTx returns raw bytes of a DepositTransaction
func DepositTx(proposer types.Identifier, blockNumber uint64, inputs []deposit.Input, nonce uint64, sigHex string) *DepositTransaction {
	sig := &DepositJSONSignature{
		JSONSignature: Sig(sigHex),
	}
	return &DepositTransaction{
		Proposer: proposer,
		Proposal: deposit.Proposal{
			BlockNumber: blockNumber,
			Inputs:      inputs,
		},
		Nonce: nonce,
		Sig:   sig,
	}
}

// RawDepositTx returns raw bytes of a DepositTransaction
func RawDepositTx(proposer types.Identifier, blockNumber uint64, inputs []deposit.Input, nonce uint64, sigHex string) []byte {
	return EncodeTx(DepositTx(proposer, blockNumber, inputs, nonce, sigHex))
}
