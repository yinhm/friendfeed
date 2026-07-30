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
	rebuildMu sync.Mutex
	Id        string
	Uuid      uuid.UUID
	bufq      []string
	iq        *queue.Queue
	itemCh    chan string
	doneCh    chan struct{}
	stoppedCh chan struct{}
	stopOnce  sync.Once
	dirty     bool
}

func NewFeedIndex(db *store.Store, id string, indexUUID uuid.UUID) *FeedIndex {
	iq := queue.New()
	index := &FeedIndex{
		Id:        id,
		Uuid:      indexUUID,
		iq:        iq,
		bufq:      make([]string, MinQueue),
		itemCh:    make(chan string, 1),
		doneCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		dirty:     false,
	}
	go index.Serve(db)
	return index
}

func (f *FeedIndex) Key() store.Key {
	return model.NewUUIDKey(model.TableMeta, f.Uuid)
}

func (f *FeedIndex) Serve(db *store.Store) {
	defer close(f.stoppedCh)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.itemCh:
			// TODO: disabled due to the last one not coming, why???
			// f.Push(uuid)
			// channel act as congestion control now,
			// if timeout rebuild frontpage faster.
		case <-ticker.C:
			f.rebuild(db)
		case <-f.doneCh:
			return
		}
	}
}

// Stop waits until the index worker has stopped using its database.
func (f *FeedIndex) Stop() {
	f.stopOnce.Do(func() {
		close(f.doneCh)
	})
	<-f.stoppedCh
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
	// Serve has one caller, but tests and maintenance code may invoke rebuild
	// directly. Serialize rebuild cycles without holding the data lock across
	// Pebble reads.
	f.rebuildMu.Lock()
	defer f.rebuildMu.Unlock()

	f.Lock()
	if !f.dirty {
		f.Unlock()
		return
	}

	oldbuf := make([]string, len(f.bufq))
	copy(oldbuf, f.bufq)

	pending := make([]string, f.iq.Length())
	for i := range pending {
		pending[i] = f.iq.Get(f.iq.Length() - i - 1).(string)
	}
	for f.iq.Length() > 0 {
		f.iq.Remove()
	}
	// A Push or markDirty during the lock-free phase sets dirty again and is
	// intentionally handled by the next rebuild.
	f.dirty = false
	f.Unlock()

	rebuilt := rebuildFeedBuffer(db, pending, oldbuf)

	f.Lock()
	f.bufq = rebuilt
	f.Unlock()
}

func rebuildFeedBuffer(db *store.Store, pending, oldbuf []string) []string {
	bufq := make([]string, MinQueue)
	index := make(map[string]struct{})

	i := 0
	appendItem := func(item string) bool {
		if _, ok := index[item]; ok {
			return false
		}
		index[item] = struct{}{}

		// skip deleted entry
		kb, _ := hex.DecodeString(item)
		if db != nil && !db.Exist(kb) {
			logger.Debugf("skip key: %s", item)
			return false
		}

		bufq[i] = item
		i++
		return i == MinQueue
	}

	for _, item := range pending {
		if appendItem(item) {
			break
		}
	}

	for _, item := range oldbuf {
		if i == MinQueue {
			break
		}
		if item == "" {
			break
		}
		if appendItem(item) {
			break
		}
	}
	return bufq
}

func (f *FeedIndex) snapshot() []string {
	f.RLock()
	defer f.RUnlock()

	bufq := make([]string, len(f.bufq))
	copy(bufq, f.bufq)
	return bufq
}

func (f *FeedIndex) load(db *store.Store) error {
	f.rebuildMu.Lock()
	defer f.rebuildMu.Unlock()
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
	f.rebuildMu.Lock()
	defer f.rebuildMu.Unlock()
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
