package services

import (
	"errors"
	"testing"
	"time"

	"github.com/muety/wakapi/mocks"
	"github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/suite"
)

type LeaderboardServiceTestSuite struct {
	suite.Suite
	Repo *mocks.LeaderboardRepositoryMock
}

func (suite *LeaderboardServiceTestSuite) BeforeTest(suiteName, testName string) {
	suite.Repo = new(mocks.LeaderboardRepositoryMock)
}

func TestLeaderboardServiceTestSuite(t *testing.T) {
	suite.Run(t, new(LeaderboardServiceTestSuite))
}

func (suite *LeaderboardServiceTestSuite) newSut() *LeaderboardService {
	return &LeaderboardService{
		cache:      cache.New(6*time.Hour, 6*time.Hour),
		repository: suite.Repo,
	}
}

// CountUsers should cache the result on success so subsequent calls skip the repo.
func (suite *LeaderboardServiceTestSuite) TestCountUsers_Success_CachesResult() {
	suite.Repo.On("CountUsers", true).Return(int64(42), nil)

	sut := suite.newSut()

	// First call: should hit repository
	count, err := sut.CountUsers(true)
	suite.Nil(err)
	suite.Equal(int64(42), count)
	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 1)

	// Second call: should hit cache, NOT repository
	count, err = sut.CountUsers(true)
	suite.Nil(err)
	suite.Equal(int64(42), count)
	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 1) // still 1
}

// CountUsers should NOT cache when the repository returns an error,
// preventing dirty/zero values from being served on subsequent calls.
func (suite *LeaderboardServiceTestSuite) TestCountUsers_Error_DoesNotCache() {
	suite.Repo.On("CountUsers", true).Return(int64(0), errors.New("db connection failed"))

	sut := suite.newSut()

	// First call: repo returns error
	count, err := sut.CountUsers(true)
	suite.NotNil(err)
	suite.Equal(int64(0), count)
	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 1)

	// Second call: should hit repo again (error must not have been cached)
	count, err = sut.CountUsers(true)
	suite.NotNil(err)
	suite.Equal(int64(0), count)
	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 2) // called again
}

// CountUsers should return the cached value directly when present,
// without calling the repository.
func (suite *LeaderboardServiceTestSuite) TestCountUsers_CacheHit_ReturnsCachedValue() {
	c := cache.New(6*time.Hour, 6*time.Hour)
	c.SetDefault("count_total_true", int64(99))

	sut := &LeaderboardService{
		cache:      c,
		repository: suite.Repo,
	}

	count, err := sut.CountUsers(true)
	suite.Nil(err)
	suite.Equal(int64(99), count)
	suite.Repo.AssertNotCalled(suite.T(), "CountUsers", true)
}

// CountUsers uses different cache keys for excludeZero=true vs false,
// so caching one should not affect the other.
func (suite *LeaderboardServiceTestSuite) TestCountUsers_DifferentExcludeZero_DifferentCacheKeys() {
	suite.Repo.On("CountUsers", true).Return(int64(10), nil)
	suite.Repo.On("CountUsers", false).Return(int64(20), nil)

	sut := suite.newSut()

	// Call with excludeZero=true
	countTrue, err := sut.CountUsers(true)
	suite.Nil(err)
	suite.Equal(int64(10), countTrue)

	// Call with excludeZero=false — should hit repo, not return the true-cached value
	countFalse, err := sut.CountUsers(false)
	suite.Nil(err)
	suite.Equal(int64(20), countFalse)

	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 2) // one per key

	// Subsequent calls: both should hit cache
	countTrue, err = sut.CountUsers(true)
	suite.Nil(err)
	suite.Equal(int64(10), countTrue)

	countFalse, err = sut.CountUsers(false)
	suite.Nil(err)
	suite.Equal(int64(20), countFalse)

	suite.Repo.AssertNumberOfCalls(suite.T(), "CountUsers", 2) // still 2
}
