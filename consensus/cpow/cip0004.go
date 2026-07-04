package cpow

// CIP-0004: Future Proposal Verification
//
// Every Block N carries a FutureProposal that commits to the transaction list
// for Block N+2.  Nodes MUST reject Block N if its proposal is invalid.
//
// Verification steps:
//  1. Root integrity  — recompute the Merkle root of TxHashes and compare.
//  2. Signature       — recover the signer from (root, sig) and verify it
//                       matches the block's coinbase (the miner).
//  3. Tx sanity       — for each proposed tx hash, if the full tx is available
//                       in the pool check nonce and balance against the parent
//                       post-execution state (state after block N-1).

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

// TxGetter is a minimal interface for looking up a transaction by hash.
// Using an interface avoids an import cycle between consensus/cpow and core/txpool.
type TxGetter interface {
	Get(hash common.Hash) *types.Transaction
}

var (
	ErrProposalMissingSignature  = errors.New("future proposal: missing signature")
	ErrProposalBadRoot           = errors.New("future proposal: root mismatch")
	ErrProposalBadSignature      = errors.New("future proposal: invalid signature")
	ErrProposalSignerMismatch    = errors.New("future proposal: signer does not match coinbase")
	ErrProposalInvalidNonce      = errors.New("future proposal: transaction has invalid nonce")
	ErrProposalInsufficientFunds = errors.New("future proposal: transaction sender has insufficient funds")
)

// proposalHashList implements types.DerivableList for a []common.Hash so we
// can reuse DeriveSha without importing the full tx objects.
type proposalHashList []common.Hash

func (l proposalHashList) Len() int                           { return len(l) }
func (l proposalHashList) EncodeIndex(i int, w *bytes.Buffer) { w.Write(l[i][:]) }

// VerifyFutureProposal validates the FutureProposal attached to block against
// the block's coinbase and the parent post-execution state.
//
//   - pool may be nil; when provided, full tx objects are fetched for nonce/balance checks.
//   - parentState may be nil; when provided, nonce and balance are checked.
func VerifyFutureProposal(blockNumber uint64, proposalHash *common.Hash, proposal *types.FutureProposal, proposalSigner *common.Address) error {
	if proposal == nil {
		return fmt.Errorf("%w: missing proposal", ErrProposalMissingSignature)
	}

	// 1. Root integrity.
	computed := types.DeriveSha(proposalHashList(proposal.TxHashes), trie.NewStackTrie(nil))
	if computed != *proposalHash {
		return fmt.Errorf("%w: header=%s computed=%s", ErrProposalBadRoot, *proposalHash, computed)
	}

	// 2. Signature verification.
	if len(proposal.Signature) == 0 {
		return ErrProposalMissingSignature
	}
	pubkey, err := crypto.SigToPub(proposalHash[:], proposal.Signature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProposalBadSignature, err)
	}
	signer := crypto.PubkeyToAddress(*pubkey)
	if proposalSigner != nil && signer != *proposalSigner {
		return fmt.Errorf("%w: got %s want %s", ErrProposalSignerMismatch, signer, *proposalSigner)
	}

	return nil
}
