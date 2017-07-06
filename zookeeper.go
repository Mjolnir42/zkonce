/*-
 * Copyright © 2017, Jörg Pernfuß <code.jpe@gmail.com>
 * All rights reserved.
 *
 * Use of this source code is governed by a 2-clause BSD license
 * that can be found in the LICENSE file.
 */

package main // import "github.com/mjolnir42/zkonce"

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

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
