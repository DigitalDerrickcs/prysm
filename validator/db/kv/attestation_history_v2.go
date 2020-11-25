package kv

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	log "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
	"go.opencensus.io/trace"
)

const (
	// The size of each data entry in bytes for the source epoch (8 bytes) and signing root (32 bytes).
	uint64Size             = 8
	latestEpochWrittenSize = uint64Size
	targetSize             = uint64Size
	sourceSize             = uint64Size
	signingRootSize        = 32
	historySize            = targetSize + sourceSize + signingRootSize
	minimalSize            = latestEpochWrittenSize
)

// HistoryData stores the needed data to confirm if an attestation is slashable
// or repeated.
type HistoryData struct {
	Source      uint64
	SigningRoot []byte
}

// Structure that represents minimal attestation source and target that are allowed to be signed.
type MinAttestation struct {
	Source uint64
	Target uint64
}

// EncHistoryData encapsulated history data.
type EncHistoryData []byte

func (hd EncHistoryData) assertSize() error {
	if hd == nil || len(hd) < minimalSize {
		return fmt.Errorf("encapsulated data size: %d is smaller then minimal size: %d", len(hd), minimalSize)
	}
	if (len(hd)-minimalSize)%historySize != 0 {
		return fmt.Errorf("encapsulated data size: %d is not a multiple of entry size: %d", len(hd), historySize)
	}
	return nil
}

func (h *HistoryData) IsEmpty() bool {
	if h == (*HistoryData)(nil) {
		return true
	}
	if h.Source == params.BeaconConfig().FarFutureEpoch {
		return true
	}
	return false
}

func emptyHistoryData() *HistoryData {
	h := &HistoryData{Source: params.BeaconConfig().FarFutureEpoch, SigningRoot: bytesutil.PadTo([]byte{}, 32)}
	return h
}

// NewAttestationHistoryArray creates a new encapsulated attestation history byte array
// sized by the latest epoch written.
func NewAttestationHistoryArray(target uint64) EncHistoryData {
	relativeTarget := target % params.BeaconConfig().WeakSubjectivityPeriod
	historyDataSize := (relativeTarget + 1) * historySize
	arraySize := latestEpochWrittenSize + historyDataSize
	en := make(EncHistoryData, arraySize)
	enc := en
	ctx := context.Background()
	var err error
	for i := uint64(0); i <= target%params.BeaconConfig().WeakSubjectivityPeriod; i++ {
		enc, err = enc.SetTargetData(ctx, i, emptyHistoryData())
		if err != nil {
			log.WithError(err).Error("Failed to set empty target data")
		}
	}
	return enc
}

func (hd EncHistoryData) GetLatestEpochWritten(ctx context.Context) (uint64, error) {
	if err := hd.assertSize(); err != nil {
		return 0, err
	}
	return bytesutil.FromBytes8(hd[:latestEpochWrittenSize]), nil
}

func (hd EncHistoryData) SetLatestEpochWritten(ctx context.Context, latestEpochWritten uint64) (EncHistoryData, error) {
	if err := hd.assertSize(); err != nil {
		return nil, err
	}
	copy(hd[:latestEpochWrittenSize], bytesutil.Uint64ToBytesLittleEndian(latestEpochWritten))
	return hd, nil
}

func (hd EncHistoryData) GetTargetData(ctx context.Context, target uint64) (*HistoryData, error) {
	if err := hd.assertSize(); err != nil {
		return nil, err
	}
	// Cursor for the location to read target epoch from.
	// Modulus of target epoch  X weak subjectivity period in order to have maximum size to the encapsulated data array.
	cursor := (target%params.BeaconConfig().WeakSubjectivityPeriod)*historySize + latestEpochWrittenSize
	if uint64(len(hd)) < cursor+historySize {
		return nil, nil
	}
	history := &HistoryData{}
	history.Source = bytesutil.FromBytes8(hd[cursor : cursor+sourceSize])
	sr := make([]byte, 32)
	copy(sr, hd[cursor+sourceSize:cursor+historySize])
	history.SigningRoot = sr
	return history, nil
}

func (hd EncHistoryData) SetTargetData(ctx context.Context, target uint64, historyData *HistoryData) (EncHistoryData, error) {
	if err := hd.assertSize(); err != nil {
		return nil, err
	}
	// Cursor for the location to write target epoch to.
	// Modulus of target epoch  X weak subjectivity period in order to have maximum size to the encapsulated data array.
	cursor := latestEpochWrittenSize + (target%params.BeaconConfig().WeakSubjectivityPeriod)*historySize

	if uint64(len(hd)) < cursor+historySize {
		ext := make([]byte, cursor+historySize-uint64(len(hd)))
		hd = append(hd, ext...)
	}
	copy(hd[cursor:cursor+sourceSize], bytesutil.Uint64ToBytesLittleEndian(historyData.Source))
	copy(hd[cursor+sourceSize:cursor+sourceSize+signingRootSize], historyData.SigningRoot)
	return hd, nil
}

// MarkAllAsAttestedSinceLatestWrittenEpoch returns an attesting history with specified target+epoch pairs
// since the latest written epoch up to the incoming attestation's target epoch as attested for.
func MarkAllAsAttestedSinceLatestWrittenEpoch(
	ctx context.Context,
	hist EncHistoryData,
	incomingTarget uint64,
	incomingAtt *HistoryData,
) (EncHistoryData, error) {
	wsPeriod := params.BeaconConfig().WeakSubjectivityPeriod
	latestEpochWritten, err := hist.GetLatestEpochWritten(ctx)
	if err != nil {
		return EncHistoryData{}, errors.Wrap(err, "could not get latest epoch written from history")
	}
	currentHD := hist
	if incomingTarget > latestEpochWritten {
		// If the target epoch to mark is ahead of latest written epoch, override the old targets and mark the requested epoch.
		// Limit the overwriting to one weak subjectivity period as further is not needed.
		maxToWrite := latestEpochWritten + wsPeriod
		for i := latestEpochWritten + 1; i < incomingTarget && i <= maxToWrite; i++ {
			newHD, err := hist.SetTargetData(ctx, i%wsPeriod, &HistoryData{
				Source: params.BeaconConfig().FarFutureEpoch,
			})
			if err != nil {
				return EncHistoryData{}, errors.Wrap(err, "could not set target data")
			}
			currentHD = newHD
		}
		newHD, err := currentHD.SetLatestEpochWritten(ctx, incomingTarget)
		if err != nil {
			return EncHistoryData{}, errors.Wrap(err, "could not set latest epoch written")
		}
		currentHD = newHD
	}
	newHD, err := currentHD.SetTargetData(ctx, incomingTarget%wsPeriod, &HistoryData{
		Source:      incomingAtt.Source,
		SigningRoot: incomingAtt.SigningRoot,
	})
	if err != nil {
		return EncHistoryData{}, errors.Wrap(err, "could not set target data")
	}
	return newHD, nil
}

// AttestationHistoryForPubKeysV2 accepts an array of validator public keys and returns a mapping of corresponding attestation history.
func (store *Store) AttestationHistoryForPubKeysV2(ctx context.Context, publicKeys [][48]byte) (map[[48]byte]EncHistoryData, map[[48]byte]MinAttestation, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.AttestationHistoryForPubKeysV2")
	defer span.End()

	if len(publicKeys) == 0 {
		return make(map[[48]byte]EncHistoryData), nil, nil
	}

	var err error
	attestationHistoryForVals := make(map[[48]byte]EncHistoryData)
	minAttForVal := make(map[[48]byte]MinAttestation)
	err = store.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricAttestationsBucket)
		for _, pk := range publicKeys {
			enc := bucket.Get(pk[:])
			var attestationHistory EncHistoryData
			if len(enc) == 0 {
				attestationHistory = NewAttestationHistoryArray(0)
			} else {
				attestationHistory = make(EncHistoryData, len(enc))
				copy(attestationHistory, enc)
			}
			var minAtt MinAttestation
			var setMin bool
			enc = bucket.Get(GetMinSourceKey(pk))
			if len(enc) != 0 {
				minAtt.Source = bytesutil.BytesToUint64BigEndian(enc)
				setMin = true
			}
			enc = bucket.Get(GetMinTargetKey(pk))
			if len(enc) != 0 {
				minAtt.Target = bytesutil.BytesToUint64BigEndian(enc)
				setMin = true
			}
			if setMin {
				minAttForVal[pk] = minAtt
			}
			attestationHistoryForVals[pk] = attestationHistory
		}
		return nil
	})
	for pk, ah := range attestationHistoryForVals {
		ehd := make(EncHistoryData, len(ah))
		copy(ehd, ah)
		attestationHistoryForVals[pk] = ehd
	}
	return attestationHistoryForVals, minAttForVal, err
}

// SaveAttestationHistoryForPubKeysV2 saves the attestation histories for the requested validator public keys.
func (store *Store) SaveAttestationHistoryForPubKeysV2(
	ctx context.Context,
	historyByPubKeys map[[48]byte]EncHistoryData,
	minByPubKeys map[[48]byte]MinAttestation,
) error {
	ctx, span := trace.StartSpan(ctx, "Validator.SaveAttestationHistoryForPubKeysV2")
	defer span.End()

	err := store.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricAttestationsBucket)
		for pubKey, encodedHistory := range historyByPubKeys {
			if err := bucket.Put(pubKey[:], encodedHistory); err != nil {
				return err
			}
		}
		for pubKey, min := range minByPubKeys {
			if err := bucket.Put(GetMinSourceKey(pubKey), bytesutil.Uint64ToBytesBigEndian(min.Source)); err != nil {
				return err
			}
			if err := bucket.Put(GetMinTargetKey(pubKey), bytesutil.Uint64ToBytesBigEndian(min.Target)); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// SaveAttestationHistoryForPubKeyV2 saves the attestation history for the requested validator public key.
func (store *Store) SaveAttestationHistoryForPubKeyV2(
	ctx context.Context,
	pubKey [48]byte,
	history EncHistoryData,
) error {
	ctx, span := trace.StartSpan(ctx, "Validator.SaveAttestationHistoryForPubKeyV2")
	defer span.End()
	return store.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricAttestationsBucket)
		return bucket.Put(pubKey[:], history)
	})
}

// SaveMinAttestation updates or saves the earliest signed attestation epoch for public key in the database.
func (store *Store) SaveMinAttestation(ctx context.Context, pubKey [48]byte, minAtt MinAttestation) error {
	ctx, span := trace.StartSpan(ctx, "Validator.SaveMinAttestation")
	defer span.End()

	return store.update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricAttestationsBucket)
		minSourceStorageKey := GetMinSourceKey(pubKey)
		enc := bucket.Get(minSourceStorageKey)
		if len(enc) == 0 {
			minSource := bytesutil.BytesToUint64BigEndian(enc)
			if minAtt.Source < minSource {
				if err := bucket.Put(minSourceStorageKey, bytesutil.Uint64ToBytesBigEndian(minAtt.Source)); err != nil {
					return err
				}
			}
		} else {
			if err := bucket.Put(minSourceStorageKey, bytesutil.Uint64ToBytesBigEndian(minAtt.Source)); err != nil {
				return err
			}
		}
		minTargetStorageKey := GetMinTargetKey(pubKey)
		enc = bucket.Get(minTargetStorageKey)
		if len(enc) == 0 {
			minTarget := bytesutil.BytesToUint64BigEndian(enc)
			if minAtt.Target < minTarget {
				if err := bucket.Put(minTargetStorageKey, bytesutil.Uint64ToBytesBigEndian(minAtt.Target)); err != nil {
					return err
				}
			}
		} else {
			if err := bucket.Put(minTargetStorageKey, bytesutil.Uint64ToBytesBigEndian(minAtt.Source)); err != nil {
				return err
			}
		}
		return nil
	})
}

// MinAttestation retrieves the earliest signed attestation epoch for public key from the database.
func (store *Store) MinAttestation(ctx context.Context, pubKey [48]byte) (*MinAttestation, error) {
	ctx, span := trace.StartSpan(ctx, "Validator.MinAttestation")
	defer span.End()
	minAtt := MinAttestation{}
	err := store.view(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(newHistoricAttestationsBucket)
		minSourceStorageKey := GetMinSourceKey(pubKey)
		enc := bucket.Get(minSourceStorageKey)
		if len(enc) == 0 {
			minAtt.Source = bytesutil.BytesToUint64BigEndian(enc)
		}
		minTargetStorageKey := GetMinTargetKey(pubKey)
		enc = bucket.Get(minTargetStorageKey)
		if len(enc) == 0 {
			minAtt.Target = bytesutil.BytesToUint64BigEndian(enc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &minAtt, nil
}
