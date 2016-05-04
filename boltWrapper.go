package main

import (
	"bytes"
	"encoding/json"
	//"fmt"
	// "io"
	"os"
	"path/filepath"
    "log"
	"github.com/boltdb/bolt"
	"encoding/binary"
)

var (
	boltEnvironmentsBucket  = []byte("environments")
	boltBuckets     = [][]byte{
		boltEnvironmentsBucket,
	}
)

var (
	boltDataVersion byte = 1
)

type BoltBackend struct {
	// Directory where data will be written. This directory will be
	// created if it doesn't exist.
	Dir string
}

func (b *BoltBackend) GetAllEnvironments() ([]*Environment, error) {
	db, err := b.db()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var result []*Environment
	var env *Environment
	err = db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(boltEnvironmentsBucket).Bucket([]byte(
			boltEnvironmentsBucket))

		// If the bucket doesn't exist, we haven't written this yet
		if bucket == nil {
			return nil
		}

	    c := bucket.Cursor()

	    count := 0
	    for k, v := c.First(); k != nil; k, v = c.Next() {
	        //fmt.Printf("key=%s, value=%s\n", k, v)
	        env = &Environment{}
	        err := b.structRead(env, v)
	        if err != nil {
				return err
	        }
	        result = append(result, env)
	        count = count+1
	    }
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (b *BoltBackend) GetEnvironment(id int) (*Environment, error) {
	db, err := b.db()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var result *Environment
	err = db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(boltEnvironmentsBucket).Bucket([]byte(
			boltEnvironmentsBucket))

		// If the bucket doesn't exist, we haven't written this yet
		if bucket == nil {
			return nil
		}

		// Get the key for this infra
		data := bucket.Get([]byte(itob(id)))
		if data == nil {
			return nil
		}

		result = &Environment{}
		return b.structRead(result, data)
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (b *BoltBackend) PutEnvironment(environment *Environment) error {

	db, err := b.db()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bolt.Tx) error {

		bucket := tx.Bucket(boltEnvironmentsBucket)
		bucket, err = bucket.CreateBucketIfNotExists([]byte(
			boltEnvironmentsBucket))
		if err != nil {
			return err
		}

		if environment.Id == 0 {
			id, _ := bucket.NextSequence()
	        environment.Id = int(id)
		}

		data, err := b.structData(environment)
		if err != nil {
			return err
		}

		return bucket.Put(itob(environment.Id), data)
	})
}

//get envs with that github repo
// append commit to changes
// store files or that change
func (b *BoltBackend) PutChange(change *Change) error {

	db, err := b.db()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bolt.Tx) error {

		bucket := tx.Bucket(boltEnvironmentsBucket)
		bucket, err = bucket.CreateBucketIfNotExists([]byte(
			boltEnvironmentsBucket))
		if err != nil {
			return err
		}

	    c := bucket.Cursor()
		var env *Environment
	    for k, v := c.First(); k != nil; k, v = c.Next() {
	        //fmt.Printf("key=%s, value=%s\n", k, v)
	        env = &Environment{}
	        err := b.structRead(env, v)
	        if err != nil {
				return err
	        }

	        if (env.Repo == change.Repository["ssh_url"] || env.Repo == change.Repository["git_url"] || env.Repo == change.Repository["git_url"] || env.Repo == change.Repository["html_url"]) {
                log.Printf("Triggering environment changes for repo: %v", env.Repo)
		        safeEnvironment := GetSingletonSafeEnvironment(env.Id)
				// TODO: Consider similar approach to http://nesv.github.io/golang/2014/02/25/worker-queues-in-go.html
                go safeEnvironment.Execute(change, "plan", 1)

				data, err := b.structData(env)
				if err != nil {
					return err
				}

				if bucket.Put(itob(env.Id), data) != nil {
					return err
				}
	        }
	    }
		return nil
	})
}

func itob(v int) []byte {
    b := make([]byte, 8)
    binary.BigEndian.PutUint64(b, uint64(v))
    return b
}


// db returns the database handle, and sets up the DB if it has never
// been created.
func (b *BoltBackend) db() (*bolt.DB, error) {
	// Make the directory to store our DB
	if err := os.MkdirAll(b.Dir, 0755); err != nil {
		return nil, err
	}

	// Create/Open the DB
	db, err := bolt.Open(filepath.Join(b.Dir, "tf-env.db"), 0644, nil)
	if err != nil {
		return nil, err
	}

	// Create the buckets
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range boltBuckets {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return db, nil
}

func (b *BoltBackend) structData(d interface{}) ([]byte, error) {
	// Let's just output it in human-readable format to make it easy
	// for debugging. Disk space won't matter that much for this data.
	return json.MarshalIndent(d, "", "\t")
}

func (b *BoltBackend) structRead(d interface{}, raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	return dec.Decode(d)
}