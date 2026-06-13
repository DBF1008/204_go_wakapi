package services

import (
	"testing"
	"time"

	"github.com/muety/wakapi/config"
	"github.com/muety/wakapi/mocks"
	"github.com/muety/wakapi/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/atomic"
)

type MiscServiceTestSuite struct {
	suite.Suite
	UserService      *mocks.UserServiceMock
	HeartbeatService *mocks.HeartbeatServiceMock
	SummaryService   *mocks.SummaryServiceMock
	KeyValueService  *mocks.KeyValueServiceMock
	MailService      *mocks.MailServiceMock
}

func (suite *MiscServiceTestSuite) BeforeTest(_, _ string) {
	suite.UserService = new(mocks.UserServiceMock)
	suite.HeartbeatService = new(mocks.HeartbeatServiceMock)
	suite.SummaryService = new(mocks.SummaryServiceMock)
	suite.KeyValueService = new(mocks.KeyValueServiceMock)
	suite.MailService = new(mocks.MailServiceMock)
}

func TestMiscServiceTestSuite(t *testing.T) {
	suite.Run(t, new(MiscServiceTestSuite))
}

// summaryArgs matches the argument list of ISummaryService.Aliased (7 args).
var summaryArgs = []interface{}{
	mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
}

// waitForUnlock blocks until the package-level countLock is released by the
// asynchronous persistence goroutine, then immediately releases it again so the
// lock is free for the next test. Observing the lock as free also guarantees the
// persistence goroutine has run to completion (its key-value writes happen before
// the deferred unlock), so any PutString assertions made afterwards are stable.
func (suite *MiscServiceTestSuite) waitForUnlock(timeout time.Duration) {
	suite.T().Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if countLock.TryLock() {
			countLock.Unlock()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	suite.FailNow("counting lock was not released within timeout")
}

// recoveringRun returns a function that runs CountTotalTime while recovering from
// any recoverable panic, reporting via the returned flag whether one occurred so a
// panic can be asserted on instead of crashing the test binary (a panic in a bare
// goroutine is otherwise unrecoverable).
//
// Note: the specific defect this guards against — unlocking a mutex that was never
// acquired on re-entry — surfaces as a *fatal* runtime error ("sync: unlock of
// unlocked mutex") that recover() cannot intercept; against the broken
// implementation it crashes the test process, which fails the test all the same.
func recoveringRun(srv *MiscService) (run func(), panicked *atomic.Bool) {
	panicked = atomic.NewBool(false)
	run = func() {
		defer func() {
			if r := recover(); r != nil {
				panicked.Store(true)
			}
		}()
		srv.CountTotalTime()
	}
	return run, panicked
}

// TestCountTotalTime_SkipsConcurrentInvocation asserts strict single-instance
// execution across the whole counting pipeline: while a run is still in flight
// (its per-user aggregation runs on a worker goroutine and the single-instance
// lock is held by the persistence goroutine), a second scheduled invocation is
// safely skipped rather than starting a duplicate count, and persistence happens
// exactly once.
//
// Against the previous implementation this fails: the lock was released as soon as
// CountTotalTime returned (before aggregation finished), so the second invocation
// would re-acquire it and run a full duplicate count (GetAll/Aliased/PutString all
// called twice).
func (suite *MiscServiceTestSuite) TestCountTotalTime_SkipsConcurrentInvocation() {
	user := &models.User{ID: "u1"}
	suite.UserService.On("GetAll").Return([]*models.User{user}, nil)

	aliasedEntered := make(chan struct{}, 8)
	release := make(chan struct{})
	suite.SummaryService.On("Aliased", summaryArgs...).
		Run(func(args mock.Arguments) {
			aliasedEntered <- struct{}{}
			<-release // hold the worker (and thus the single-instance lock) until released
		}).
		Return(&models.Summary{}, nil)
	suite.KeyValueService.On("PutString", mock.Anything).Return(nil)

	sut := NewMiscService(suite.UserService, suite.HeartbeatService, suite.SummaryService, suite.KeyValueService, suite.MailService)
	run, panicked := recoveringRun(sut)

	// First invocation: acquires the lock and dispatches the per-user worker job,
	// which blocks inside Aliased. The detached persistence goroutine keeps the
	// single-instance lock held for the whole window.
	go run()
	select {
	case <-aliasedEntered:
	case <-time.After(5 * time.Second):
		suite.FailNow("first invocation never reached per-user aggregation")
	}

	// Second invocation while the first pipeline is still in flight: must be skipped
	// immediately (returns before fetching users again) and must not panic.
	run()
	suite.False(panicked.Load(), "concurrent invocation must not panic")
	suite.UserService.AssertNumberOfCalls(suite.T(), "GetAll", 1)
	suite.SummaryService.AssertNumberOfCalls(suite.T(), "Aliased", 1)

	// Let the first pipeline finish and persist, then verify it released the lock.
	close(release)
	suite.waitForUnlock(5 * time.Second)

	suite.False(panicked.Load())
	suite.SummaryService.AssertNumberOfCalls(suite.T(), "Aliased", 1)
	suite.KeyValueService.AssertNumberOfCalls(suite.T(), "PutString", 2)
	suite.KeyValueService.AssertCalled(suite.T(), "PutString", &models.KeyStringValue{Key: config.KeyLatestTotalTime, Value: "0s"})
	suite.KeyValueService.AssertCalled(suite.T(), "PutString", &models.KeyStringValue{Key: config.KeyLatestTotalUsers, Value: "1"})
}

// TestCountTotalTime_NoUnlockPanicOnConcurrentEntry reproduces the reported panic
// directly: when two invocations race at entry, the loser must be skipped, not run
// on through to a deferred unlock of a mutex it never acquired.
//
// The first invocation is parked inside the (gated) GetAll call while holding the
// lock; a second invocation is then started. Against the previous implementation
// the second invocation ignored the failed TryLock, ran on, and its unconditional
// `defer countLock.Unlock()` unlocked the first invocation's lock, causing a
// "sync: unlock of unlocked mutex" panic (and breaking mutual exclusion). The fix
// makes the second invocation return immediately.
func (suite *MiscServiceTestSuite) TestCountTotalTime_NoUnlockPanicOnConcurrentEntry() {
	user := &models.User{ID: "u1"}

	getAllEntered := make(chan struct{}, 8)
	release := make(chan struct{})
	suite.UserService.On("GetAll").
		Run(func(args mock.Arguments) {
			getAllEntered <- struct{}{}
			<-release // park inside the critical section while holding the lock
		}).
		Return([]*models.User{user}, nil)
	suite.SummaryService.On("Aliased", summaryArgs...).Return(&models.Summary{}, nil)
	suite.KeyValueService.On("PutString", mock.Anything).Return(nil)

	sut := NewMiscService(suite.UserService, suite.HeartbeatService, suite.SummaryService, suite.KeyValueService, suite.MailService)
	run, panicked := recoveringRun(sut)

	// First invocation acquires the lock and blocks inside GetAll.
	done1 := make(chan struct{})
	go func() { run(); close(done1) }()
	select {
	case <-getAllEntered:
	case <-time.After(5 * time.Second):
		suite.FailNow("first invocation never reached GetAll")
	}

	// Second invocation races at entry while the first holds the lock. With the fix
	// it returns immediately (skipped); the broken version would block in the gated
	// GetAll and then panic on its deferred unlock once released.
	done2 := make(chan struct{})
	go func() { run(); close(done2) }()

	// Release so any parked invocation can proceed (and, in the broken version, hit
	// the double-unlock panic which recoveringRun captures).
	close(release)

	suite.waitForChan(done1, 5*time.Second, "first invocation did not return")
	suite.waitForChan(done2, 5*time.Second, "second invocation did not return")
	suite.waitForUnlock(5 * time.Second)

	suite.False(panicked.Load(), "concurrent entry must not cause an unlock-of-unlocked-mutex panic")
	suite.UserService.AssertNumberOfCalls(suite.T(), "GetAll", 1)
}

func (suite *MiscServiceTestSuite) waitForChan(ch <-chan struct{}, timeout time.Duration, msg string) {
	suite.T().Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		suite.FailNow(msg)
	}
}
