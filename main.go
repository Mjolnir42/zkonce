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
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/client9/reopen"
	"github.com/davecgh/go-spew/spew"
	"github.com/droundy/goopt"
	"github.com/mjolnir42/erebos"
	"github.com/samuel/go-zookeeper/zk"
)

var startNode, finishNode, runLock string

func init() {
	// set standard logger options
	erebos.SetLogrusOptions()
}

func main() {
	// parse command line flags
	cliConfPath := goopt.String([]string{`-c`, `--config`},
		`/etc/zkonce/zkonce.conf`, `Configuration file`)
	job := goopt.String([]string{`-j`, `--job`},
		``, `Job name to run command under`)
	per := goopt.String([]string{`-p`, `--per`},
		`day`, `Duration per which to run the command once`)
	goopt.Parse(nil)

	switch *per {
	case `day`, `hour`:
	default:
		logrus.Fatalln(`Invalid per duration -p|--per`)
	}

	if *job == `` {
		logrus.Fatalln(`Invalid empty jobname -j|--job`)
	}

	// read runtime configuration
	conf := Config{}
	if err := conf.FromFile(*cliConfPath); err != nil {
		logrus.Fatalf("Could not open configuration: %s", err)
	}

	// setup logfile
	if lfh, err := reopen.NewFileWriter(conf.LogFile); err != nil {
		logrus.Fatalf("Unable to open logfile: %s", err)
	} else {
		logrus.SetOutput(lfh)
	}
	logrus.Infoln(`Running zkonce`)

	conn, chroot := connect(conf.Ensemble)
	defer conn.Close()
	logrus.Infoln(`chroot:`, chroot)

	flags := int32(0)
	acl := zk.WorldACL(zk.PermAll)

	chrootParts := strings.Split(chroot, `/`)
	for i := range chrootParts {
		p := filepath.Join(append(
			[]string{`/`}, chrootParts[0:i+1]...)...)
		if p == `/` {
			continue
		}
		path, err := conn.Create(p, []byte{}, flags, acl)
		if err != zk.ErrNodeExists {
			assertOK(err)
		}
		if path != `` {
			logrus.Infof("Created %s", path)
		} else {
			logrus.Infof("Path %s exists", p)
		}
	}
	zkOncePath := filepath.Join(chroot, `/zkonce`)
	path, err := conn.Create(zkOncePath, []byte{}, flags, acl)
	if err != zk.ErrNodeExists {
		assertOK(err)
	}
	if path != `` {
		logrus.Infof("Created %s", path)
	} else {
		logrus.Infof("Path %s exists", zkOncePath)
	}
	zkOncePath = filepath.Join(zkOncePath, `/`, conf.SyncGroup)
	path, err = conn.Create(zkOncePath, []byte{}, flags, acl)
	if err != zk.ErrNodeExists {
		assertOK(err)
	}
	if path != `` {
		logrus.Infof("Created %s", path)
	} else {
		logrus.Infof("Path %s exists", zkOncePath)
	}

	startNode = filepath.Join(zkOncePath, `/start`)
	path, err = conn.Create(startNode, []byte{}, flags, acl)
	if err != zk.ErrNodeExists {
		assertOK(err)
	}
	if path != `` {
		logrus.Infof("Created %s", path)
	} else {
		logrus.Infof("Path %s exists", startNode)
	}

	finishNode = filepath.Join(zkOncePath, `/finish`)
	path, err = conn.Create(finishNode, []byte{}, flags, acl)
	if err != zk.ErrNodeExists {
		assertOK(err)
	}
	if path != `` {
		logrus.Infof("Created %s", path)
	} else {
		logrus.Infof("Path %s exists", finishNode)
	}

	runLock = filepath.Join(zkOncePath, `/runlock`)
	path, err = conn.Create(runLock, []byte{}, flags, acl)
	if err != zk.ErrNodeExists {
		assertOK(err)
	}
	if path != `` {
		logrus.Infof("Created %s", path)
	} else {
		logrus.Infof("Path %s exists", runLock)
	}

	isLeader := false
	election := filepath.Join(runLock, `zkonce-`)
	path, err = conn.Create(election, []byte{}, int32(
		zk.FlagEphemeral|zk.FlagSequence), acl)
	assertOK(err)
	logrus.Infof("Created %s", path)

	_, election = filepath.Split(path)

	children, _, event, err := conn.ChildrenW(runLock)
	sort.Strings(children)
	if children[0] == election {
		isLeader = true
		leader(conn)
	}

eventrecv:
	if !isLeader {
		ev := <-event
		switch ev.Type {
		case zk.EventNodeChildrenChanged:
			children, _, event, err = conn.ChildrenW(runLock)
			sort.Strings(children)
			if children[0] == election {
				isLeader = true
				leader(conn)
			}
			goto eventrecv
		}
	}

	<-time.After(60 * time.Second)
	logrus.Infof("Shutting down")
}

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
	conn, _, err := zk.Connect(zks, 3*time.Second)
	assertOK(err)

	return conn, chroot
}

func leader(conn *zk.Conn) {
	fmt.Println("I AM THE LEADER")
	run := false

	val, s, err := conn.Get(startNode)
	assertOK(err)
	version := s.Version

	var startTime time.Time
	if len(val) > 0 {
		err = startTime.UnmarshalText(val)
		assertOK(err)
	}

	if startTime.IsZero() {
		fmt.Println(`START TIME IS ZERO`)
		run = true
	} else {
		diff := time.Now().UTC().Sub(startTime)
		if diff > time.Hour {
			run = true
		}
	}

	if !run {
		logrus.Infoln(`not running`)
		return
	}
	nowTime := time.Now().UTC().Format(time.RFC3339Nano)
	s, err = conn.Set(startNode, []byte(nowTime), version)
	assertOK(err)
	if s.Version > version {
		logrus.Infoln(`startNode version increased to`, s.Version)
	}

	cmdSlice := []string{}
	for i := range os.Args {
		if os.Args[i] == `--` {
			cmdSlice = os.Args[i+1:]
			break
		}
	}
	if len(cmdSlice) == 0 {
		return
	}
	cmd := exec.Command(cmdSlice[0], cmdSlice[1:]...)
	fmt.Println(`Running:`, cmdSlice)

	// TODO: check current uid os.Getuid - only root can
	// switch uids. also allow if specified user is current
	// user.
	user, err := user.Lookup(`nobody`)
	assertOK(err)
	uid, _ := strconv.Atoi(user.Uid)
	gid, _ := strconv.Atoi(user.Gid)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(uid),
			Gid:    uint32(gid),
			Groups: []uint32{},
		},
	}
	out, err := cmd.Output()
	assertOK(err)
	fmt.Println(`Output:`, string(out))

	_, s, err = conn.Get(finishNode)
	assertOK(err)
	version = s.Version
	nowTime = time.Now().UTC().Format(time.RFC3339Nano)
	s, err = conn.Set(finishNode, []byte(nowTime), version)
	assertOK(err)
	if s.Version > version {
		logrus.Infoln(`finishNode version increased to`, s.Version)
	}
}

// vim: ts=4 sw=4 sts=4 noet fenc=utf-8 ffs=unix
