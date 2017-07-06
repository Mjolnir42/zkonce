/*-
 * Copyright © 2017, Jörg Pernfuß <code.jpe@gmail.com>
 * All rights reserved.
 *
 * Use of this source code is governed by a 2-clause BSD license
 * that can be found in the LICENSE file.
 */

package main // import "github.com/mjolnir42/zkonce"

import (
	"github.com/Sirupsen/logrus"
	"github.com/davecgh/go-spew/spew"
)

func validDuration(per *string) {
	switch *per {
	case `day`, `hour`:
	default:
		logrus.Fatalf("Invalid per duration -p|--per spec: %s", per)
	}
}

func assertOK(err error) {
	if err != nil {
		spew.Dump(err)
		logrus.Fatalf("ASSERT ERROR: %s", err.Error())
	}
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
