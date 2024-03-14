package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"log"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

var fromPath string
var toPath string
var command string

func init() {
	flag.StringVar(&fromPath, "from", "", "from directory")
	flag.StringVar(&toPath, "to", "", "to directory")
	flag.StringVar(&command, "c", "", "command to do")
}

func main() {
	flag.Parse()

	db := store.NewStore(fromPath)
	mdb := store.NewMetaStore(fromPath + "/meta")
	ndb := store.NewStore(toPath)

	ndb.SetSync(false)

	// opt := ndb.Options()
	// log.Println(opt.MemTableSize)

	switch command {
	case "db":
		prefix := []byte("")

		log.Println("iter db now...")

		// iter here
		iter := db.NewIterator(prefix)
		for iter.First(); iter.Valid(); iter.Next() {
			ndb.Set(iter.Key(), iter.Value())
		}
		iter.Close()
		ndb.Flush()
		log.Println("iter done...")
	case "meta":
		prefix := []byte("")

		log.Println("iter meta db now...")

		// iter here
		iter := mdb.NewIterator(prefix)
		for iter.First(); iter.Valid(); iter.Next() {
			ndb.Set(iter.Key(), iter.Value())
		}
		iter.Close()
		ndb.Flush()
		log.Println("iter done...")
	case "sync":
		// EntryIndex
		model.EntryIndex.Iter(db, func(k, v []byte) error {
			return ndb.Set(k, v)
		})
		ndb.Flush()
		log.Println("iter done...")

		// Entry
		i := 0
		err := model.Entry.Iter(db, func(k, v []byte) error {
			i++
			return ndb.Set(k, v)
		})
		if err != nil {
			log.Println(err)
		}
		log.Println("synced entry count: ", i)
		ndb.Flush()
		log.Println("entry iter done...")

	case "debug":
		profile, err := model.GetProfileFromUserId(mdb, "yinhm")
		if err != nil {
			log.Println(err)
		}
		log.Println(profile)

		profile, err = model.GetProfileFromUserId(mdb, "veronicabelmont")
		if err != nil {
			log.Println(err)
		}
		log.Println(profile)

		entryId := "c8e5fe86df10e285f1ff12acac102d44"
		// entryId := "0000012820141462fa7290a658763ae1"
		entry, err := model.GetEntry(db, entryId)
		if err != nil {
			log.Println(err)
		}
		log.Println(entry)

		entry, err = model.GetEntry(ndb, entryId)
		if err != nil {
			log.Println(err)
		}
		log.Println(entry)

		model.Entry.Iter(ndb, func(k, v []byte) error {
			log.Println(model.Entry.ToStringKey(k))
			log.Println(hex.EncodeToString(k), hex.EncodeToString(v))

			return errors.New("stop iter")
		})
	}

	ndb.Close()
	mdb.Close()
	db.Close()
}
