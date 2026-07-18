// Copyright 2015 The Lastff Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package store

import "os"

func mkdir(path string) error {
	return os.MkdirAll(path, 0700)
}
