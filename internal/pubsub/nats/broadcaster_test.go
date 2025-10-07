package nats

import (
	"dexcelerate/internal/config"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gitlab.com/nevasik7/alerting/logger"
)

// MockLogger implements logger.Logger for tests
type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Debug(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Debugf(msg string, args ...interface{}) {
	m.Called(msg, args)
}

func (m *MockLogger) Info(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Warn(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Warnf(msg string, args ...interface{}) {
	m.Called(msg, args)
}

func (m *MockLogger) Error(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Fatal(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Fatalf(msg string, args ...interface{}) {
	m.Called(msg, args)
}

func (m *MockLogger) Panic(msg string) {
	m.Called(msg)
}

func (m *MockLogger) Panicf(msg string, args ...interface{}) {
	m.Called(msg, args)
}

func (m *MockLogger) WithField(key string, value interface{}) logger.Logger {
	m.Called(key, value)
	return m
}

func (m *MockLogger) WithFields(fields map[string]interface{}) logger.Logger {
	m.Called(fields)
	return m
}

func (m *MockLogger) Infof(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.Called(format, args)
}

// ------------------------ tests not real connection ------------------------
func TestConnect_NilConfig(t *testing.T) {
	mockLogger := new(MockLogger)

	client, err := Connect(nil, mockLogger)

	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Equal(t, "config is required", err.Error())
	mockLogger.AssertNotCalled(t, "Infof", mock.Anything, mock.Anything)
}

func TestConnect_EmptyURL(t *testing.T) {
	mockLogger := new(MockLogger)

	cfg := &config.Config{}
	cfg.PubSub.NATS.URL = ""

	client, err := Connect(cfg, mockLogger)

	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Equal(t, "nats url is required", err.Error())
	mockLogger.AssertNotCalled(t, "Infof", mock.Anything, mock.Anything)
}

func TestReady_NilConnection(t *testing.T) {
	mockLogger := new(MockLogger)

	client := &Client{
		nc:  nil,
		log: mockLogger,
	}

	assert.False(t, client.Ready())
}

func TestStatus_NilConnection(t *testing.T) {
	mockLogger := new(MockLogger)
	client := &Client{
		nc:  nil,
		log: mockLogger,
	}

	// execute and verify
	assert.Equal(t, nats.DISCONNECTED, client.Status())
}

func TestClose_NilConnection(t *testing.T) {
	mockLogger := new(MockLogger)
	client := &Client{
		nc:  nil,
		log: mockLogger,
	}

	err := client.Close()

	assert.NoError(t, err)
	mockLogger.AssertNotCalled(t, "Errorf", mock.Anything, mock.Anything)
	mockLogger.AssertNotCalled(t, "Infof", mock.Anything, mock.Anything)
}

// ------------------------ tests not real connection ------------------------

// ------------------------ tests in-memory nats connection ------------------------
func runTestWithInMemoryNATS(t *testing.T, testFunc func(*testing.T, *server.Server, string)) {
	t.Helper()

	// run in-memory NATS server
	opts := natsserver.DefaultTestOptions
	opts.Port = -1 // random port
	s := natsserver.RunServer(&opts)
	defer s.Shutdown()

	// give server time running
	time.Sleep(100 * time.Millisecond)

	// run test func with server and his URL
	testFunc(t, s, s.ClientURL())
}

func TestConnect_Success(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)

		require.NoError(t, err)
		require.NotNil(t, client)
		assert.True(t, client.Ready())
		assert.Equal(t, nats.CONNECTED, client.Status())

		mockLogger.AssertExpectations(t)

		// cleanup not use client.Close() because that avoid the unexpected call Infof
		if client != nil && client.nc != nil {
			client.nc.Close()
		}
	})
}

func TestConnect_WithBroadcastPrefix(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url
		cfg.PubSub.NATS.BroadcastPrefix = "broadcast.test"

		client, err := Connect(cfg, mockLogger)

		require.NoError(t, err)
		require.NotNil(t, client)
		assert.True(t, client.Ready())

		mockLogger.AssertExpectations(t)

		if client != nil && client.nc != nil {
			client.nc.Close()
		}
	})
}

func TestClose_Success(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()
		mockLogger.On("Infof", "NATS connection closed gracefully", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)
		require.NoError(t, err)

		err = client.Close()
		assert.NoError(t, err)

		// check what conn real close
		assert.False(t, client.Ready())
		assert.Equal(t, nats.CLOSED, client.Status())

		mockLogger.AssertExpectations(t)
	})
}

func TestReady_States(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)
		require.NoError(t, err)

		// check what conn ready
		assert.True(t, client.Ready())
		assert.Equal(t, nats.CONNECTED, client.Status())

		client.nc.Close()
		assert.False(t, client.Ready())
		assert.Equal(t, nats.CLOSED, client.Status())

		mockLogger.AssertExpectations(t)
	})
}

func TestStatus_VariousStates(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)
		require.NoError(t, err)

		assert.Equal(t, nats.CONNECTED, client.Status())

		client.nc.Close()
		assert.Equal(t, nats.CLOSED, client.Status())

		mockLogger.AssertExpectations(t)
	})
}

func TestClose_Idempotent(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()
		mockLogger.On("Infof", "NATS connection closed gracefully", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)
		require.NoError(t, err)

		err = client.Close()
		assert.NoError(t, err)

		err = client.Close()
		assert.NoError(t, err)

		err = client.Close()
		assert.NoError(t, err)

		mockLogger.AssertNumberOfCalls(t, "Infof", 2) // connect + close
	})
}

func TestReconnectBehavior(t *testing.T) {
	runTestWithInMemoryNATS(t, func(t *testing.T, s *server.Server, url string) {
		mockLogger := new(MockLogger)
		mockLogger.On("Infof", "Connected to NATS successfully, url=%s", mock.Anything).Once()

		cfg := &config.Config{}
		cfg.PubSub.NATS.URL = url

		client, err := Connect(cfg, mockLogger)
		require.NoError(t, err)

		assert.True(t, client.Ready())

		client.nc.Close()
		mockLogger.AssertExpectations(t)
	})
}

// ------------------------ tests in-memory nats connection ------------------------
