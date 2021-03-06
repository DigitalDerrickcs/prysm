// Package iface defines an interface for the validator database.
package iface

import (
	"context"
	"io"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/backuputil"
	"github.com/prysmaticlabs/prysm/validator/db/kv"
)

// Ensure the kv store implements the interface.
var _ = ValidatorDB(&kv.Store{})

// ValidatorDB defines the necessary methods for a Prysm validator DB.
type ValidatorDB interface {
	io.Closer
	backuputil.BackupExporter
	DatabasePath() string
	ClearDB() error
	RunUpMigrations(ctx context.Context) error
	RunDownMigrations(ctx context.Context) error
	UpdatePublicKeysBuckets(publicKeys [][48]byte) error

	// Genesis information related methods.
	GenesisValidatorsRoot(ctx context.Context) ([]byte, error)
	SaveGenesisValidatorsRoot(ctx context.Context, genValRoot []byte) error

	// Proposer protection related methods.
	HighestSignedProposal(ctx context.Context, publicKey [48]byte) (uint64, bool, error)
	LowestSignedProposal(ctx context.Context, publicKey [48]byte) (uint64, bool, error)
	ProposalHistoryForPubKey(ctx context.Context, publicKey [48]byte) ([]*kv.Proposal, error)
	ProposalHistoryForSlot(ctx context.Context, publicKey [48]byte, slot uint64) ([32]byte, bool, error)
	SaveProposalHistoryForSlot(ctx context.Context, pubKey [48]byte, slot uint64, signingRoot []byte) error
	ProposedPublicKeys(ctx context.Context) ([][48]byte, error)

	// Attester protection related methods.
	// Methods to store and read blacklisted public keys from EIP-3076
	// slashing protection imports.
	EIPImportBlacklistedPublicKeys(ctx context.Context) ([][48]byte, error)
	SaveEIPImportBlacklistedPublicKeys(ctx context.Context, publicKeys [][48]byte) error
	SigningRootAtTargetEpoch(ctx context.Context, publicKey [48]byte, target uint64) ([32]byte, error)
	LowestSignedTargetEpoch(ctx context.Context, publicKey [48]byte) (uint64, bool, error)
	LowestSignedSourceEpoch(ctx context.Context, publicKey [48]byte) (uint64, bool, error)
	AttestedPublicKeys(ctx context.Context) ([][48]byte, error)
	CheckSlashableAttestation(
		ctx context.Context, pubKey [48]byte, signingRoot [32]byte, att *ethpb.IndexedAttestation,
	) (kv.SlashingKind, error)
	SaveAttestationForPubKey(
		ctx context.Context, pubKey [48]byte, signingRoot [32]byte, att *ethpb.IndexedAttestation,
	) error
	SaveAttestationsForPubKey(
		ctx context.Context, pubKey [48]byte, signingRoots [][32]byte, atts []*ethpb.IndexedAttestation,
	) error
	AttestationHistoryForPubKey(
		ctx context.Context, pubKey [48]byte,
	) ([]*kv.AttestationRecord, error)
}
