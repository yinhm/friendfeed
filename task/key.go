package task

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
)

const (
	TaskIDSize          = len(flake.Id{})
	TaskIDHexSize       = TaskIDSize * 2
	MaxTaskTypeBytes    = 64
	MaxIdempotencyBytes = 256
)

func EncodeTaskID(id flake.Id) string {
	return hex.EncodeToString(id[:])
}

func DecodeTaskID(value string) (flake.Id, error) {
	var id flake.Id
	if len(value) != TaskIDHexSize {
		return id, fmt.Errorf("task id length %d, want %d", len(value), TaskIDHexSize)
	}
	if value != strings.ToLower(value) {
		return id, errors.New("task id must be lowercase hex")
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return id, fmt.Errorf("decode task id: %w", err)
	}
	copy(id[:], raw)
	if err := validateTaskID(id); err != nil {
		return flake.Id{}, err
	}
	return id, nil
}

func ValidateType(taskType string) error {
	if len(taskType) == 0 || len(taskType) > MaxTaskTypeBytes {
		return fmt.Errorf("task type length %d is outside 1..%d", len(taskType), MaxTaskTypeBytes)
	}
	for _, c := range []byte(taskType) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("task type %q contains invalid byte 0x%02x", taskType, c)
	}
	return nil
}

func TaskKey(id flake.Id) (store.Key, error) {
	if err := validateTaskID(id); err != nil {
		return nil, err
	}
	return model.NewKeyFrom(model.Task.Prefix, id[:]), nil
}

func ParseTaskKey(key []byte) (flake.Id, error) {
	return parseIDKey("Task", model.Task.Prefix, key)
}

func ReadyTypePrefix(taskType string) (store.Key, error) {
	if err := ValidateType(taskType); err != nil {
		return nil, err
	}
	return model.NewKeyFrom(model.TaskReady.Prefix, []byte{byte(len(taskType))}, []byte(taskType)), nil
}

func ReadyKey(taskType string, runAtMS int64, id flake.Id) (store.Key, error) {
	if err := validateTaskID(id); err != nil {
		return nil, err
	}
	prefix, err := ReadyTypePrefix(taskType)
	if err != nil {
		return nil, err
	}
	timeBytes, err := encodeTime(runAtMS)
	if err != nil {
		return nil, err
	}
	return model.NewKeyFrom(prefix, timeBytes, id[:]), nil
}

func ParseReadyKey(key []byte) (taskType string, runAtMS int64, id flake.Id, err error) {
	prefixLen := model.TaskReady.Prefix.Len()
	if len(key) < prefixLen+1 || !bytes.Equal(key[:prefixLen], model.TaskReady.Prefix) {
		err = errors.New("invalid TaskReady prefix or truncated key")
		return
	}
	typeLen := int(key[prefixLen])
	want := prefixLen + 1 + typeLen + 8 + TaskIDSize
	if typeLen == 0 || len(key) != want {
		err = fmt.Errorf("invalid TaskReady key length %d, want %d", len(key), want)
		return
	}
	taskType = string(key[prefixLen+1 : prefixLen+1+typeLen])
	if err = ValidateType(taskType); err != nil {
		return
	}
	offset := prefixLen + 1 + typeLen
	runAtMS, err = decodeTime(key[offset : offset+8])
	if err != nil {
		return
	}
	copy(id[:], key[offset+8:])
	err = validateTaskID(id)
	return
}

func LeaseKey(leaseUntilMS int64, id flake.Id) (store.Key, error) {
	if err := validateTaskID(id); err != nil {
		return nil, err
	}
	timeBytes, err := encodeTime(leaseUntilMS)
	if err != nil {
		return nil, err
	}
	return model.NewKeyFrom(model.TaskLease.Prefix, timeBytes, id[:]), nil
}

func ParseLeaseKey(key []byte) (leaseUntilMS int64, id flake.Id, err error) {
	return parseTimeIDKey("TaskLease", model.TaskLease.Prefix, key)
}

func IdemKey(taskType, idempotencyKey string) (store.Key, error) {
	if err := ValidateType(taskType); err != nil {
		return nil, err
	}
	if len(idempotencyKey) == 0 || len(idempotencyKey) > MaxIdempotencyBytes {
		return nil, fmt.Errorf("idempotency key length %d is outside 1..%d", len(idempotencyKey), MaxIdempotencyBytes)
	}
	digest := sha256.Sum256([]byte(taskType + "\x00" + idempotencyKey))
	return model.NewKeyFrom(model.TaskIdem.Prefix, digest[:]), nil
}

func DoneKey(finishedAtMS int64, id flake.Id) (store.Key, error) {
	if err := validateTaskID(id); err != nil {
		return nil, err
	}
	timeBytes, err := encodeTime(finishedAtMS)
	if err != nil {
		return nil, err
	}
	return model.NewKeyFrom(model.TaskDone.Prefix, timeBytes, id[:]), nil
}

func ParseDoneKey(key []byte) (finishedAtMS int64, id flake.Id, err error) {
	return parseTimeIDKey("TaskDone", model.TaskDone.Prefix, key)
}

func parseIDKey(name string, prefix store.Key, key []byte) (flake.Id, error) {
	var id flake.Id
	want := prefix.Len() + TaskIDSize
	if len(key) != want || !bytes.Equal(key[:prefix.Len()], prefix) {
		return id, fmt.Errorf("invalid %s key length or prefix", name)
	}
	copy(id[:], key[prefix.Len():])
	if err := validateTaskID(id); err != nil {
		return flake.Id{}, err
	}
	return id, nil
}

func parseTimeIDKey(name string, prefix store.Key, key []byte) (int64, flake.Id, error) {
	var id flake.Id
	want := prefix.Len() + 8 + TaskIDSize
	if len(key) != want || !bytes.Equal(key[:prefix.Len()], prefix) {
		return 0, id, fmt.Errorf("invalid %s key length or prefix", name)
	}
	at, err := decodeTime(key[prefix.Len() : prefix.Len()+8])
	if err != nil {
		return 0, id, err
	}
	copy(id[:], key[prefix.Len()+8:])
	if err := validateTaskID(id); err != nil {
		return 0, flake.Id{}, err
	}
	return at, id, nil
}

func ParseIdemKey(key []byte) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	want := model.TaskIdem.Prefix.Len() + sha256.Size
	if len(key) != want || !bytes.Equal(key[:model.TaskIdem.Prefix.Len()], model.TaskIdem.Prefix) {
		return digest, errors.New("invalid TaskIdem key length or prefix")
	}
	copy(digest[:], key[model.TaskIdem.Prefix.Len():])
	return digest, nil
}

func validateTaskID(id flake.Id) error {
	if id == (flake.Id{}) {
		return errors.New("task id must not be zero")
	}
	return nil
}

func encodeTime(ms int64) ([]byte, error) {
	if ms < 0 {
		return nil, fmt.Errorf("task time %d is before Unix epoch", ms)
	}
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(ms))
	return raw[:], nil
}

func decodeTime(raw []byte) (int64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("task time length %d, want 8", len(raw))
	}
	value := binary.BigEndian.Uint64(raw)
	if value > uint64(^uint64(0)>>1) {
		return 0, errors.New("task time overflows int64")
	}
	return int64(value), nil
}
