package server

// optimize for public feed
import (
	"bytes"
	"encoding/gob"
	"sync"
	"time"

	queue "github.com/eapache/queue"
	uuid "github.com/satori/go.uuid"
	store "github.com/yinhm/friendfeed/storage"
)

const MinQueue = 500

type FeedIndex struct {
	sync.Mutex
	Id     string
	Uuid   *uuid.UUID
	bufq   []string
	iq     *queue.Queue
	itemCh chan string
	doneCh chan struct{}
	dirty  bool
}

func NewFeedIndex(id string, uuid1 *uuid.UUID) *FeedIndex {
	iq := queue.New()
	index := &FeedIndex{
		Id:     id,
		Uuid:   uuid1,
		iq:     iq,
		bufq:   make([]string, MinQueue),
		itemCh: make(chan string, 1),
		doneCh: make(chan struct{}, 1),
		dirty:  false,
	}
	go index.Serve()
	return index
}

// key to dump index cache to db
func (f *FeedIndex) Key() store.Key {
	return store.NewUUIDKey(store.TableIndexCache, *f.Uuid)
}

func (f *FeedIndex) Serve() {
	timeout := 1 * time.Second
	for {
		select {
		case <-f.itemCh:
			// TODO: disabled due to the last one not coming, why???
			// f.Push(uuid)
			// channel act as congestion control now,
			// if timeout rebuild frontpage faster.
		case <-time.After(timeout):
			f.rebuild()
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

	f.itemCh <- uuid
}

func (f *FeedIndex) Remove(key1 string) {
	f.Lock()
	defer f.Unlock()

	index := -1
	for i, key2 := range f.bufq {
		if key1 == key2 {
			index = i
			break
		}
	}
	if index >= 0 {
		f.bufq = append(f.bufq[:index], f.bufq[index+1:]...)
	}
}

func (f *FeedIndex) rebuild() {
	if !f.dirty {
		return
	}

	f.Lock()
	defer f.Unlock()

	oldbuf := make([]string, MinQueue)
	copy(oldbuf, f.bufq)

	f.bufq = make([]string, MinQueue)
	index := make(map[string]struct{})

	i := 0
	for j := 0; j < f.iq.Length(); j++ {
		item := f.iq.Get(f.iq.Length() - j - 1).(string)
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

	for j := 0; i < MinQueue; j++ {
		item := oldbuf[j]
		if item == "" {
			break
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

func (f *FeedIndex) load(db *store.Store) error {
	f.Lock()
	defer f.Unlock()

	key := f.Key()
	rawdata, err := db.Get(key.Bytes())
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
	return db.Put(f.Key().Bytes(), buf.Bytes())
}
