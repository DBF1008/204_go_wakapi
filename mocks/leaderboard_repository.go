package mocks

import (
	"github.com/muety/wakapi/models"
	"github.com/stretchr/testify/mock"
)

type LeaderboardRepositoryMock struct {
	BaseRepositoryMock
	mock.Mock
}

func (m *LeaderboardRepositoryMock) InsertBatch(items []*models.LeaderboardItem) error {
	args := m.Called(items)
	return args.Error(0)
}

func (m *LeaderboardRepositoryMock) CountAllByUser(userId string) (int64, error) {
	args := m.Called(userId)
	return args.Get(0).(int64), args.Error(1)
}

func (m *LeaderboardRepositoryMock) CountUsers(excludeZero bool) (int64, error) {
	args := m.Called(excludeZero)
	return args.Get(0).(int64), args.Error(1)
}

func (m *LeaderboardRepositoryMock) DeleteByUser(userId string) error {
	args := m.Called(userId)
	return args.Error(0)
}

func (m *LeaderboardRepositoryMock) DeleteByUserAndInterval(userId string, interval *models.IntervalKey) error {
	args := m.Called(userId, interval)
	return args.Error(0)
}

func (m *LeaderboardRepositoryMock) GetAll() ([]*models.LeaderboardItem, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.LeaderboardItem), args.Error(1)
}

func (m *LeaderboardRepositoryMock) GetAllAggregatedByInterval(interval *models.IntervalKey, by *uint8, limit, offset int) ([]*models.LeaderboardItemRanked, error) {
	args := m.Called(interval, by, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.LeaderboardItemRanked), args.Error(1)
}

func (m *LeaderboardRepositoryMock) GetAggregatedByUserAndInterval(userId string, interval *models.IntervalKey, by *uint8, limit, offset int) ([]*models.LeaderboardItemRanked, error) {
	args := m.Called(userId, interval, by, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.LeaderboardItemRanked), args.Error(1)
}
