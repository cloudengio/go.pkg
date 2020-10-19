// Copyright 2020 cloudeng llc. All rights reserved.
// Use of this source code is governed by the Apache-2.0
// license that can be found in the LICENSE file.

// Package localdb provides an implementation of filewalk.Database that
// uses a local key/value store.
package localdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"

	"cloudeng.io/errors"
	"cloudeng.io/file/filewalk"
	"cloudeng.io/os/lockedfile"
	"github.com/recoilme/pudge"
)

const (
	globalStatsKey   = "__globalStats"
	usersListKey     = "__userList"
	prefixdbFilename = "prefix.pudge"
	statsdbFilename  = "stats.pudge"
	userdbFilename   = "users.pudge"
	errordbFilename  = "errors.pudge"
	dbLockName       = "db.lock"
	dbLockerInfoName = "db.info"
)

// Database represents an on-disk database that stores information
// and statistics for filesystem directories/prefixes. The database
// supports read-write and read-only modes of access.
type Database struct {
	opts               options
	dir                string
	prefixdb           *pudge.Db
	statsdb            *pudge.Db
	errordb            *pudge.Db
	userdb             *pudge.Db
	dbLockFilename     string
	dbLockInfoFilename string
	dbMutex            *lockedfile.Mutex
	unlockFn           func()
	globalStats        *statsCollection
	userStats          *perUserStats
}

// ErrReadonly is returned if an attempt is made to write to a database
// opened in read-only mode.
var ErrReadonly = errors.New("database is opened in readonly mode")

// DatabaseOption represents a specific option accepted by Open.
type DatabaseOption func(o *Database)

type options struct {
	readOnly            bool
	resetStats          bool
	syncIntervalSeconds int
	lockRetryDelay      time.Duration
	tryLock             bool
}

// SyncInterval set the interval at which the database is to be
// persisted to disk.
func SyncInterval(interval time.Duration) DatabaseOption {
	return func(db *Database) {
		if interval == 0 {
			db.opts.syncIntervalSeconds = 60
			return
		}
		i := interval + (500 * time.Millisecond)
		db.opts.syncIntervalSeconds = int(i.Round(time.Second).Seconds())
	}
}

// TryLock returns an error if the database cannot be locked within
// the delay period.
func TryLock() DatabaseOption {
	return func(db *Database) {
		db.opts.tryLock = true
	}
}

// LockStatusDelay sets the delay between checking the status of acquiring a
// lock on the database.
func LockStatusDelay(d time.Duration) DatabaseOption {
	return func(db *Database) {
		db.opts.lockRetryDelay = d
	}
}

type lockFileContents struct {
	User string `json:"user"`
	CWD  string `json:"current_directory"`
	PPID int    `json:"parent_process_pid"`
	PID  int    `json:"process_pid"`
}

func readLockerInfo(filename string) (lockFileContents, error) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return lockFileContents{}, err
	}
	var contents lockFileContents
	err = json.Unmarshal(buf, &contents)
	return contents, err
}

func writeLockerInfo(filename string) error {
	cwd, _ := os.Getwd()
	pid := os.Getpid()
	ppid := os.Getppid()
	contents := lockFileContents{
		User: os.Getenv("USER"),
		CWD:  cwd,
		PID:  pid,
		PPID: ppid,
	}
	buf, err := json.Marshal(contents)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, buf, 0666)
}

func newDB(dir string) *Database {
	db := &Database{
		dir:                dir,
		dbLockFilename:     filepath.Join(dir, dbLockName),
		dbLockInfoFilename: filepath.Join(dir, dbLockerInfoName),
		globalStats:        newStatsCollection(globalStatsKey),
		userStats:          newPerUserStats(),
	}
	db.opts.lockRetryDelay = time.Second * 5
	os.MkdirAll(dir, 0770)
	db.dbMutex = lockedfile.MutexAt(db.dbLockFilename)
	return db
}

func lockerInfo(filename string) string {
	info, err := readLockerInfo(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return err.Error()
		}
		return fmt.Sprintf("failed to obtain locker info from %v: %v", filename, err)
	}
	str, _ := json.MarshalIndent(info, "", "  ")
	return string(str)
}

func lockerErrorInfo(dir, filename string, err error) error {
	return fmt.Errorf("failed to lock %v: %v\nlock info from: %v:\n%v", dir, err, filename, lockerInfo(filename))
}

func (db *Database) acquireLock(ctx context.Context, readOnly bool, tryDelay time.Duration, tryLock bool) error {
	type lockResult struct {
		unlock func()
		err    error
	}
	lockType := "write "
	if readOnly {
		lockType = "read "
	}
	ch := make(chan lockResult)
	go func() {
		var unlock func()
		var err error
		if readOnly {
			unlock, err = db.dbMutex.RLock()
		} else {
			unlock, err = db.dbMutex.Lock()
		}
		ch <- lockResult{unlock, err}
	}()
	for {
		select {
		case lr := <-ch:
			if lr.err == nil {
				db.unlockFn = lr.unlock
				if readOnly {
					return nil
				}
				return writeLockerInfo(db.dbLockInfoFilename)
			}
			return lockerErrorInfo(db.dir, db.dbLockInfoFilename, lr.err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tryDelay):
			if tryLock {
				err := fmt.Errorf("failed to acquire %slock after %v", lockType, tryDelay)
				return lockerErrorInfo(db.dir, db.dbLockInfoFilename, err)
			}
			fmt.Fprintf(os.Stderr, "waiting to acquire %slock: lock info from %s:\n", lockType, db.dbLockInfoFilename)
			fmt.Fprintf(os.Stderr, "%s\n", lockerInfo(db.dbLockInfoFilename))
			tryDelay *= 2
			if tryDelay > time.Minute*10 {
				tryDelay = time.Minute * 10
			}
		}
	}
}

func (db *Database) unlock() error {
	err := os.Remove(db.dbLockInfoFilename)
	db.unlockFn()
	return err
}

func Open(ctx context.Context, dir string, ifcOpts []filewalk.DatabaseOption, opts ...DatabaseOption) (filewalk.Database, error) {
	db := newDB(dir)
	var dbOpts filewalk.DatabaseOptions
	for _, fn := range ifcOpts {
		fn(&dbOpts)
	}
	db.opts.readOnly = dbOpts.ReadOnly
	db.opts.resetStats = dbOpts.ResetStats
	db.opts.lockRetryDelay = time.Minute
	for _, fn := range opts {
		fn(db)
	}

	err := db.acquireLock(ctx, db.opts.readOnly, db.opts.lockRetryDelay, db.opts.tryLock)

	if err != nil {
		return nil, err
	}

	cfg := pudge.Config{
		StoreMode:    0,
		FileMode:     0666,
		DirMode:      0777,
		SyncInterval: db.opts.syncIntervalSeconds,
	}
	if db.opts.readOnly {
		cfg.SyncInterval = 0
	}

	db.prefixdb, err = pudge.Open(filepath.Join(dir, prefixdbFilename), &cfg)
	if err != nil {
		return nil, err
	}
	db.statsdb, err = pudge.Open(filepath.Join(dir, statsdbFilename), &cfg)
	if err != nil {
		db.closeAll(ctx)
		return nil, err
	}
	db.errordb, err = pudge.Open(filepath.Join(dir, errordbFilename), &cfg)
	if err != nil {
		db.closeAll(ctx)
		return nil, err
	}
	db.userdb, err = pudge.Open(filepath.Join(dir, userdbFilename), &cfg)
	if err != nil {
		db.closeAll(ctx)
		return nil, err
	}
	if db.opts.resetStats {
		return db, nil
	}
	if err := db.globalStats.loadOrInit(db.statsdb, globalStatsKey); err != nil {
		db.closeAll(ctx)
		return nil, fmt.Errorf("failed to load stats: %v", err)
	}
	if err := db.userStats.loadUserList(db.userdb); err != nil {
		db.closeAll(ctx)
		return nil, fmt.Errorf("failed to load user list: %v", err)
	}
	return db, nil
}

func (db *Database) closeAll(ctx context.Context) error {
	errs := errors.M{}
	closer := func(db *pudge.Db) {
		if db != nil {
			errs.Append(db.Close())
		}
	}
	closer(db.prefixdb)
	closer(db.statsdb)
	closer(db.errordb)
	closer(db.userdb)
	db.unlock()
	return errs.Err()
}

func (db *Database) saveStats() error {
	if db.opts.readOnly {
		return ErrReadonly
	}
	return db.globalStats.save(db.statsdb, globalStatsKey)
}

func (db *Database) Close(ctx context.Context) error {
	if !db.opts.readOnly {
		return db.Save(ctx)
	}
	return db.closeAll(ctx)
}

func (db *Database) Save(ctx context.Context) error {
	if db.opts.readOnly {
		return ErrReadonly
	}
	errs := errors.M{}
	errs.Append(db.globalStats.save(db.statsdb, globalStatsKey))
	errs.Append(db.userStats.save(db.userdb))
	errs.Append(db.closeAll(ctx))
	return errs.Err()
}

func (db *Database) Set(ctx context.Context, prefix string, info *filewalk.PrefixInfo) error {
	if db.opts.readOnly {
		return ErrReadonly
	}
	db.globalStats.update(prefix, info)
	errs := errors.M{}
	errs.Append(db.prefixdb.Set(prefix, info))
	errs.Append(db.userStats.updateUserStats(db.userdb, prefix, info))
	err := errs.Err()
	switch {
	case err == nil && len(info.Err) == 0:
		return nil
	case err == nil && len(info.Err) != 0:
		// TODO(cnicolaou): build a lexicon of layout and user info
		// to save space on disk.
		errs.Append(db.errordb.Set(prefix, &info))
		return errs.Err()
	}
	ninfo := &filewalk.PrefixInfo{}
	if len(info.Err) != 0 {
		ninfo.Err = fmt.Sprintf("%v: failed to write to database: %v", info.Err, err)
	} else {
		ninfo.Err = fmt.Sprintf("failed to write to database: %v", err)
	}
	errs.Append(db.errordb.Set(prefix, ninfo))
	return errs.Err()
}

func (db *Database) Get(ctx context.Context, prefix string, info *filewalk.PrefixInfo) (bool, error) {
	if err := db.prefixdb.Get(prefix, info); err != nil {
		if err == pudge.ErrKeyNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (db *Database) NewScanner(prefix string, limit int, opts ...filewalk.ScannerOption) filewalk.DatabaseScanner {
	return NewScanner(db, prefix, limit, opts)
}

func (db *Database) UserIDs(ctx context.Context) ([]string, error) {
	return db.userStats.users, nil
}

func getMetricNames() []filewalk.MetricName {
	metrics := []filewalk.MetricName{
		filewalk.TotalFileCount,
		filewalk.TotalPrefixCount,
		filewalk.TotalDiskUsage}
	sort.Slice(metrics, func(i, j int) bool {
		return string(metrics[i]) < string(metrics[j])
	})
	return metrics
}

func (db *Database) Metrics() []filewalk.MetricName {
	return getMetricNames()
}

func metricOptions(opts []filewalk.MetricOption) filewalk.MetricOptions {
	var o filewalk.MetricOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

func (db *Database) Total(ctx context.Context, name filewalk.MetricName, opts ...filewalk.MetricOption) (int64, error) {
	o := metricOptions(opts)
	if o.Global {
		return db.globalStats.total(name)
	}
	sc, err := db.userStats.statsForUser(db.userdb, o.UserID)
	if err != nil {
		return -1, err
	}
	return sc.total(name)
}

func (sc *statsCollection) total(name filewalk.MetricName) (int64, error) {
	switch name {
	case filewalk.TotalFileCount:
		return sc.NumFiles.Sum(), nil
	case filewalk.TotalPrefixCount:
		return sc.NumChildren.Sum(), nil
	case filewalk.TotalDiskUsage:
		return sc.DiskUsage.Sum(), nil
	}
	return -1, fmt.Errorf("unsupported metric: %v", name)
}

func (db *Database) TopN(ctx context.Context, name filewalk.MetricName, n int, opts ...filewalk.MetricOption) ([]filewalk.Metric, error) {
	o := metricOptions(opts)
	if o.Global {
		return db.globalStats.topN(name, n)
	}
	sc, err := db.userStats.statsForUser(db.userdb, o.UserID)
	if err != nil {
		return nil, err
	}
	return sc.topN(name, n)
}

func topNMetrics(top []struct {
	K string
	V int64
}) []filewalk.Metric {
	m := make([]filewalk.Metric, len(top))
	for i, kv := range top {
		m[i] = filewalk.Metric{Prefix: kv.K, Value: kv.V}
	}
	return m
}

func (sc *statsCollection) topN(name filewalk.MetricName, n int) ([]filewalk.Metric, error) {
	switch name {
	case filewalk.TotalFileCount:
		return topNMetrics(sc.NumFiles.TopN(n)), nil
	case filewalk.TotalPrefixCount:
		return topNMetrics(sc.NumChildren.TopN(n)), nil
	case filewalk.TotalDiskUsage:
		return topNMetrics(sc.DiskUsage.TopN(n)), nil
	}
	return nil, fmt.Errorf("unsupported metric: %v", name)
}