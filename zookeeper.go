/*-
 * Copyright © 2017, Jörg Pernfuß <code.jpe@gmail.com>
 * All rights reserved.
 *
 * Use of this source code is governed by a 2-clause BSD license
 * that can be found in the LICENSE file.
 */

package main // import "github.com/mjolnir42/zkonce"

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/samuel/go-zookeeper/zk"
)

func connect(cstr string) (*zk.Conn, string) {
	var servers, chroot string
	sr := strings.SplitN(cstr, `/`, 2)

	switch len(sr) {
	case 0:
		assertOK(fmt.Errorf(`Empty zk ensemble!`))
	case 1:
		servers = sr[0]
	case 2:
		servers = sr[0]
		chroot = `/` + sr[1]
	}
	zks := strings.Split(servers, `,`)
	conn, _, err := zk.Connect(zks, 6*time.Second)
	assertOK(err)

	return conn, chroot
}

func zkHier(conn *zk.Conn, hier string, existsOK bool) bool {
	hierParts := strings.Split(hier, `/`)

	for i := range hierParts {
		part := filepath.Join(append(
			[]string{`/`}, hierParts[0:i+1]...)...)

		// root always exists
		if part == `/` {
			continue
		}
		// create node
		if !zkCreatePath(conn, part, existsOK) {
			return false
		}
	}
	return true
}

func zkCreatePath(conn *zk.Conn, path string, existsOK bool) bool {
	createdPath, err := conn.Create(path, []byte{}, int32(0), zk.WorldACL(zk.PermAll))
	if err != zk.ErrNodeExists || !existsOK {
		if errorOK(err) {
			return false
		}
	}
	if createdPath != `` {
		logrus.Infof("Created zk node %s", createdPath)
	}
	return true
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
