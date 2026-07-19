package server

// optimize for public feed
import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"sync"
	"time"

	"github.com/eapache/queue"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

const MinQueue = 1000

type FeedIndex struct {
	sync.RWMutex
	Id     string
	Uuid   uuid.UUID
	bufq   []string
	iq     *queue.Queue
	itemCh chan string
	doneCh chan struct{}
	dirty  bool
}

func NewFeedIndex(db *store.Store, id string, indexUUID uuid.UUID) *FeedIndex {
	iq := queue.New()
	index := &FeedIndex{
		Id:     id,
		Uuid:   indexUUID,
		iq:     iq,
		bufq:   make([]string, MinQueue),
		itemCh: make(chan string, 1),
		doneCh: make(chan struct{}, 1),
		dirty:  false,
	}
	go index.Serve(db)
	return index
}

func (f *FeedIndex) Key() store.Key {
	return model.NewUUIDKey(model.TableMeta, f.Uuid)
}

func (f *FeedIndex) Serve(db *store.Store) {
	timeout := 1 * time.Second
	for {
		select {
		case <-f.itemCh:
			// TODO: disabled due to the last one not coming, why???
			// f.Push(uuid)
			// channel act as congestion control now,
			// if timeout rebuild frontpage faster.
		case <-time.After(timeout):
			f.rebuild(db)
		case <-f.doneCh:
			// close(f.itemCh)
			close(f.doneCh)
			return
		}
	}
}

func (f *FeedIndex) Push(uuid string) {
	f.Lock()
	f.iq.Add(uuid)
	f.dirty = true
	f.Unlock()

	select {
	case f.itemCh <- uuid:
	default:
	}
}

func (f *FeedIndex) remove(i int) {
	copy(f.bufq[i:], f.bufq[i+1:])
	f.bufq = f.bufq[:len(f.bufq)-1]
}

func (f *FeedIndex) rebuild(db *store.Store) {
	f.Lock()
	defer f.Unlock()
	if !f.dirty {
		return
	}

	oldbuf := make([]string, MinQueue)
	copy(oldbuf, f.bufq)

	f.bufq = make([]string, MinQueue)
	index := make(map[string]struct{})

	i := 0
	for j := 0; j < f.iq.Length(); j++ {
		item := f.iq.Get(f.iq.Length() - j - 1).(string)

		// skip deleted entry
		kb, _ := hex.DecodeString(item)
		if db != nil && !db.Exist(kb) {
			logger.Debugf("skip key: %s", item)
			continue
		}

		if _, ok := index[item]; !ok {
			index[item] = struct{}{}
			f.bufq[i] = item
			i++
		}
		if i == MinQueue {
			break
		}
	}

	// TODO: should we shrink queue cap?
	for f.iq.Length() > 0 {
		f.iq.Remove()
	}

	for j := 0; j < len(oldbuf) && i < MinQueue; j++ {
		item := oldbuf[j]
		if item == "" {
			break
		}

		// skip deleted entry
		kb, _ := hex.DecodeString(item)
		if db != nil && !db.Exist(kb) {
			logger.Debugf("skip key: %s", item)
			continue
		}

		if _, ok := index[item]; !ok {
			index[item] = struct{}{}
			f.bufq[i] = item
			i++
		}
		if i == MinQueue {
			break
		}
	}

	f.dirty = false
}

func (f *FeedIndex) snapshot() []string {
	f.RLock()
	defer f.RUnlock()

	bufq := make([]string, len(f.bufq))
	copy(bufq, f.bufq)
	return bufq
}

func (f *FeedIndex) load(db *store.Store) error {
	f.Lock()
	defer f.Unlock()

	key := f.Key()
	logger.Debugf("load local cache: %s", key.String())
	rawdata, err := db.Get(key)
	if err != nil {
		return err
	}
	if len(rawdata) == 0 {
		return nil
	}

	buf := bytes.NewBuffer(rawdata)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&f.bufq)
	if err != nil {
		logger.Debugf("error while loading cache: %s", err)
		return err
	}
	return nil
}

func (f *FeedIndex) dump(db *store.Store) error {
	f.Lock()
	defer f.Unlock()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(f.bufq)
	if err != nil {
		return err
	}
	return db.Put(f.Key(), buf.Bytes())
}

func (f *FeedIndex) markDirty() {
	f.Lock()
	f.dirty = true
	f.Unlock()
}
