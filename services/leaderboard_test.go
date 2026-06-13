package services

import (
	"testing"

	"github.com/leandro-lugaresi/hub"
	"github.com/muety/wakapi/config"
	"github.com/muety/wakapi/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type LeaderboardServiceTestSuite struct {
	suite.Suite
	LeaderboardRepository *mocks.LeaderboardRepositoryMock
	SummaryService        *mocks.SummaryServiceMock
	UserService           *mocks.UserServiceMock
}

func (suite *LeaderboardServiceTestSuite) SetupSuite() {
	cfg := config.Empty()
	// must be a valid interval, otherwise NewLeaderboardService calls Fatal
	cfg.App.LeaderboardScope = "7_days"
	config.Set(cfg)
}

func (suite *LeaderboardServiceTestSuite) BeforeTest(suiteName, testName string) {
	suite.LeaderboardRepository = new(mocks.LeaderboardRepositoryMock)
	suite.SummaryService = new(mocks.SummaryServiceMock)
	suite.UserService = new(mocks.UserServiceMock)
}

func (suite *LeaderboardServiceTestSuite) createSut() *LeaderboardService {
	// swap in a throwaway event bus so the constructor's subscription works in isolation
	originalEventBus := config.EventBus()
	eventBus := hub.New()
	config.SetEventBus(eventBus)
	sut := NewLeaderboardService(suite.LeaderboardRepository, suite.SummaryService, suite.UserService)
	config.SetEventBus(originalEventBus)
	return sut
}

func TestLeaderboardServiceTestSuite(t *testing.T) {
	suite.Run(t, new(LeaderboardServiceTestSuite))
}

// TestLeaderboardService_CountUsers_CachesOnSuccess verifies the successful result is
// served from cache on subsequent calls, so the repository is only hit once.
func (suite *LeaderboardServiceTestSuite) TestLeaderboardService_CountUsers_CachesOnSuccess() {
	sut := suite.createSut()
	// .Once() asserts the repository is consulted exactly once; a second hit would be
	// an unexpected call and fail the test (which is exactly the pre-fix behaviour).
	suite.LeaderboardRepository.On("CountUsers", true).Return(int64(42), nil).Once()

	count1, err1 := sut.CountUsers(true)
	count2, err2 := sut.CountUsers(true)

	assert.NoError(suite.T(), err1)
	assert.NoError(suite.T(), err2)
	assert.Equal(suite.T(), int64(42), count1)
	assert.Equal(suite.T(), int64(42), count2)
	suite.LeaderboardRepository.AssertNumberOfCalls(suite.T(), "CountUsers", 1)
	suite.LeaderboardRepository.AssertExpectations(suite.T())
}

// TestLeaderboardService_CountUsers_DoesNotCacheOnError is the core regression test:
// a failed lookup must not poison the cache. The pre-fix code cached the (zero) count on
// error, so a later call would wrongly return (0, nil) from cache without re-querying.
func (suite *LeaderboardServiceTestSuite) TestLeaderboardService_CountUsers_DoesNotCacheOnError() {
	sut := suite.createSut()

	// first call: repository fails -> must surface the error and cache nothing
	suite.LeaderboardRepository.On("CountUsers", true).Return(int64(0), assert.AnError).Once()
	count1, err1 := sut.CountUsers(true)
	assert.Error(suite.T(), err1)
	assert.Equal(suite.T(), int64(0), count1)

	// second call: repository now succeeds -> it MUST be queried again and return the
	// fresh value, proving the earlier error did not get cached.
	suite.LeaderboardRepository.On("CountUsers", true).Return(int64(7), nil).Once()
	count2, err2 := sut.CountUsers(true)
	assert.NoError(suite.T(), err2)
	assert.Equal(suite.T(), int64(7), count2)

	suite.LeaderboardRepository.AssertNumberOfCalls(suite.T(), "CountUsers", 2)
	suite.LeaderboardRepository.AssertExpectations(suite.T())
}

// TestLeaderboardService_CountUsers_SeparateCacheKeysPerExcludeZero ensures the
// excludeZero flag produces distinct cache entries so the two variants don't collide.
func (suite *LeaderboardServiceTestSuite) TestLeaderboardService_CountUsers_SeparateCacheKeysPerExcludeZero() {
	sut := suite.createSut()
	suite.LeaderboardRepository.On("CountUsers", true).Return(int64(5), nil).Once()
	suite.LeaderboardRepository.On("CountUsers", false).Return(int64(9), nil).Once()

	countTrue, errTrue := sut.CountUsers(true)
	countFalse, errFalse := sut.CountUsers(false)
	// repeat both to confirm each variant is cached independently
	countTrueCached, _ := sut.CountUsers(true)
	countFalseCached, _ := sut.CountUsers(false)

	assert.NoError(suite.T(), errTrue)
	assert.NoError(suite.T(), errFalse)
	assert.Equal(suite.T(), int64(5), countTrue)
	assert.Equal(suite.T(), int64(9), countFalse)
	assert.Equal(suite.T(), int64(5), countTrueCached)
	assert.Equal(suite.T(), int64(9), countFalseCached)
	suite.LeaderboardRepository.AssertNumberOfCalls(suite.T(), "CountUsers", 2)
	suite.LeaderboardRepository.AssertExpectations(suite.T())
}
