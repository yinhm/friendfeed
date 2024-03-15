package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

var fromPath string
var toPath string
var command string

func init() {
	flag.StringVar(&fromPath, "from", "", "from directory")
	flag.StringVar(&toPath, "to", "", "to directory")
	flag.StringVar(&command, "c", "", "command to do")
}

func purge_table(db *store.Store, prefix store.Key) (int, error) {
	return db.ForwardScan(prefix, func(i int, k, v []byte) error {
		return db.Delete(k)
	})
}

// We should open original db ad readonly mode.
//
// migrate meta db should and only need sync_meta
// ./tools -from old_db -to new_db -c sync_meta
//
// sync all data from db
// ./tools -from old_db -to new_db -c db
// sync all data from meta db
// ./tools -from old_db -to new_db -c meta
//
// Test Profile
// ./tools -from old_db -to new_db -c profile
// debug
// ./tools -from old_db -to new_db -c debug
func main() {
	flag.Parse()

	db := store.NewStore(fromPath)
	mdb := store.NewMetaStore(fromPath + "/meta")
	ndb := store.NewStore(toPath)

	db.Options()
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

		prefix = model.TableProfile.Bytes()
		n, err := mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			return ndb.Set(k, v)
		})
		if err != nil {
			log.Println("Error on scanning user profiles:", err)
		}
		log.Printf("Profiles: %d", n)
		ndb.Flush()

		// now test it
		n, err = mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			v2, err := ndb.Get(k)
			if err != nil {
				fmt.Println("value not fount")
				return err
			}

			if !bytes.Equal(v, v2) {
				fmt.Println(v)
				fmt.Println(v2)
				return errors.New("value not equal")
			}
			return nil
		})
		if err != nil {
			log.Println("Error on compare profiles:", err)
		}
		log.Printf("Profiles: %d", n)

	case "sync_meta":
		log.Println("scan meta db now...")

		n, err := mdb.ForwardScan(model.TableProfile.Bytes(), func(i int, k, v []byte) error {
			msg := &pb.Profile{}
			if err := proto.Unmarshal(v, msg); err != nil {
				return err
			} else {
				// ndb.Set(k, v)
				return model.UpdateProfile(ndb, msg) // use UpdateProfile also update id->uuid map
			}
		})
		if err != nil {
			log.Println(err)
		}
		log.Println("profile count: ", n)

		n, err = mdb.ForwardScan(model.TableOAuth.Bytes(), func(i int, k, v []byte) error {
			msg := &pb.OAuthUser{}
			if err := proto.Unmarshal(v, msg); err != nil {
				return err
			}
			if msg.UserId != "" && msg.AccessToken != "" && msg.AccessTokenSecret != "" && msg.Provider != "" {
				log.Println(hex.EncodeToString(k), msg)
				model.PutOAuthUser(ndb, msg)
			}
			return nil
		})
		if err != nil {
			log.Println(err)
		}
		log.Println("oauth user count: ", n)
		ndb.Flush()
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
	case "profile":
		prefix := model.TableProfile
		j := 0
		n, err := mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
			profile := &pb.Profile{}
			if err := proto.Unmarshal(v, profile); err != nil {
				return err
			}
			if profile.Id == "yinhm" {
				fmt.Printf("profile: %s\n", profile)

				new_value, _ := ndb.Get(k)
				// pb messge differ
				// if !bytes.Equal(v, new_value) {
				// 	fmt.Println(v)
				// 	fmt.Println(new_value)
				// 	return errors.New("value not equal")
				// }
				if err := proto.Unmarshal(new_value, profile); err != nil {
					return err
				}
				fmt.Printf("new profile: %s\n", profile)

				return nil
			}
			// fmt.Printf("profile: <%s, %s>\n", profile.Uuid, profile.Id)
			// fmt.Println(profile)

			j++
			return nil
		})
		if err != nil {
			fmt.Println("Error on scanning user profiles:", err)
		}
		fmt.Printf("Profiles: %d, %d have services.\n", n, j)

	case "purge_profile":
		n, err := purge_table(ndb, model.TableProfile.Bytes())
		if err != nil {
			fmt.Println("Error on scanning user profiles:", err)
		}
		fmt.Printf("Profiles: %d has been removed.\n", n)
		ndb.Flush()

	case "purge_oauth":
		n, err := purge_table(ndb, model.TableOAuth.Bytes())
		if err != nil {
			fmt.Println("Error on scanning oauth:", err)
		}
		fmt.Printf("oauth: %d has been removed.\n", n)
		ndb.Flush()

	case "count_meta":
		prefix := []byte("")
		log.Println("iter meta db now...")

		// prefix = model.TableProfile.Bytes()
		n, _ := mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			return nil
		})
		log.Printf("key counts: %d", n)

		// now test it
		// n, _ = mdb.ForwardScan(model.TableProfile.Bytes(), func(i int, k, v []byte) error {
		// 	return nil
		// })
		// count += n
		// n, _ = mdb.ForwardScan(model.TableOAuth.Bytes(), func(i int, k, v []byte) error {
		// 	return nil
		// })
		// count += n
		// log.Printf("key counts: %d", count)

		count := 0
		count_parsed := 0
		mdb.ForwardScan(prefix, func(i int, k, v []byte) error {
			msg := &pb.OAuthUser{}
			if err := proto.Unmarshal(v, msg); err == nil {
				count_parsed++
				// model.PutOAuthUser(ndb, msg) // oauth format updated
			} else {
				msg := &pb.Profile{}
				if err := proto.Unmarshal(v, msg); err == nil {
					count_parsed++
					// ndb.Set(k, v) // profile
				} else {
					count++
					uuid1, err := uuid.FromBytes(v)
					if err != nil {
						fmt.Println(k, v) // cache
					} else {
						v2, _ := model.UserMap.GetRaw(ndb, k)
						fmt.Println(string(k), uuid1.String(), bytes.Equal(v, v2))
						// update id->uuid map here
					}
				}
			}
			return nil
		})
		fmt.Printf("known keys: %d id->uuid map keys: %d\n", count_parsed, count)

	case "debug":
		// map changed, this will fail
		profile, err := model.GetProfileFromUserId(mdb, "yinhm")
		if err != nil {
			log.Println(err)
		}
		log.Println(profile)

		// profile, err = model.GetProfileFromUserId(mdb, "veronicabelmont")
		// if err != nil {
		// 	log.Println(err)
		// }
		// log.Println(profile)

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

		// First entry
		// model.Entry.Iter(ndb, func(k, v []byte) error {
		// 	log.Println(model.Entry.ToStringKey(k))
		// 	log.Println(hex.EncodeToString(k), hex.EncodeToString(v))

		// 	return errors.New("stop iter")
		// })

		// test oauth user in new db
		_, msg, err := model.GetOAuthUser(ndb, "twitter", "5289142")
		if err != nil && err != model.ErrNotFound {
			log.Printf("oauth user not found: %s", err)
		}
		log.Printf("oauth user: <%s>", msg)

		model.OAuth.Iter(ndb, func(k, v []byte) error {
			log.Println(model.Entry.ToStringKey(k))
			log.Println(hex.EncodeToString(k), hex.EncodeToString(v))

			return errors.New("stop iter")
		})

		// uuidStr := "f82871b4-6b05-510a-9ae1-b626addf5b09"
		// profile, err := model.GetProfileFromUuid(db, uuidStr)
		// if err != nil {
		// 	log.Println(err)
		// }
		// log.Println(profile)

		v, _ := model.UserMap.GetRaw(ndb, []byte("yinhm"))
		log.Printf("id map: <%s>", v)
	}

	ndb.Close()
	mdb.Close()
	db.Close()
}
