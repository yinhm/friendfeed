// Copyright 2015 The Lastff Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package store

import "os"

func path_exists(path string) (bool, error) {
	_, err := os.Stat(path)

	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

func mkdir(path string) error {
	exists, err := path_exists(path)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	if err = os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return nil
}
