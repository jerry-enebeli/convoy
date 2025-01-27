package task

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/frain-dev/convoy"
	"github.com/frain-dev/convoy/auth/realm_chain"
	"github.com/frain-dev/convoy/datastore"
	"github.com/frain-dev/convoy/queue"
	"github.com/go-redis/redis_rate/v9"
	"github.com/hibiken/asynq"
	"github.com/jarcoal/httpmock"

	"github.com/frain-dev/convoy/config"
	"github.com/frain-dev/convoy/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestProcessEventDelivery(t *testing.T) {
	tt := []struct {
		name          string
		cfgPath       string
		expectedError error
		msg           *datastore.EventDelivery
		dbFn          func(*mocks.MockApplicationRepository, *mocks.MockGroupRepository, *mocks.MockEventDeliveryRepository, *mocks.MockRateLimiter, *mocks.MockSubscriptionRepository, *mocks.MockQueuer)
		nFn           func() func()
	}{
		{
			name:          "Event already sent.",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any())
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{RetryConfig: &datastore.DefaultRetryConfig}, nil)

				o.EXPECT().FetchGroupByID(gomock.Any(), gomock.Any()).Return(&datastore.Group{}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       0,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.SuccessEventStatus,
					}, nil).Times(1)
			},
		},
		{
			name:          "Endpoint is inactive",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.InactiveSubscriptionStatus,
					}, nil)

				o.EXPECT().FetchGroupByID(gomock.Any(), gomock.Any()).Return(&datastore.Group{Config: &datastore.GroupConfig{
					RateLimit: &datastore.DefaultRateLimitConfig,
					Strategy:  &datastore.DefaultStrategyConfig,
				}}, nil)
				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       0,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
		},
		{
			name:          "Endpoint does not respond with 2xx",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: &EndpointError{Err: ErrDeliveryAttemptFailed, delay: 20 * time.Second},
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       0,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.ScheduledEventStatus,
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(400, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Max retries reached - do not disable subscription - failed",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       2,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.ScheduledEventStatus,
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: false,
						},
					}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Max retries reached - disabled subscription - failed",
			cfgPath:       "./testdata/Config/basic-convoy-disable-endpoint.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       2,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.ScheduledEventStatus,
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				s.EXPECT().
					UpdateSubscriptionStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Manual retry - no disable endpoint - failed",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       3,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.ScheduledEventStatus,
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.StrategyProvider("default"),
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: false,
						},
					}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(400, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Manual retry - disable endpoint - failed",
			cfgPath:       "./testdata/Config/basic-convoy-disable-endpoint.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       3,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
						Status: datastore.ScheduledEventStatus,
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				s.EXPECT().
					UpdateSubscriptionStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(400, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Manual retry - no disable endpoint - success",
			cfgPath:       "./testdata/Config/basic-convoy.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Status: datastore.ScheduledEventStatus,
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       4,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: false,
						},
					}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
		{
			name:          "Manual retry - disable endpoint - success",
			cfgPath:       "./testdata/Config/basic-convoy-disable-endpoint.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID: "123",
					}, nil).Times(1)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Status: datastore.ScheduledEventStatus,
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       4,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				s.EXPECT().UpdateSubscriptionStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},

		{
			name:          "Manual retry - send disable endpoint notification",
			cfgPath:       "./testdata/Config/basic-convoy-disable-endpoint.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID:      "123",
						SupportEmail: "test@gmail.com",
					}, nil).Times(1)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.ActiveSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Status: datastore.ScheduledEventStatus,
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       4,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				s.EXPECT().UpdateSubscriptionStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				q.EXPECT().
					Write(convoy.NotificationProcessor, convoy.DefaultQueue, gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},

		{
			name:          "Manual retry - send endpoint enabled notification",
			cfgPath:       "./testdata/Config/basic-convoy-disable-endpoint.json",
			expectedError: nil,
			msg: &datastore.EventDelivery{
				UID: "",
			},
			dbFn: func(a *mocks.MockApplicationRepository, o *mocks.MockGroupRepository, m *mocks.MockEventDeliveryRepository, r *mocks.MockRateLimiter, s *mocks.MockSubscriptionRepository, q *mocks.MockQueuer) {
				a.EXPECT().FindApplicationEndpointByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Endpoint{
						RateLimit:         10,
						TargetURL:         "https://google.com",
						RateLimitDuration: "1m",
					}, nil).Times(1)
				a.EXPECT().FindApplicationByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Application{
						GroupID:      "123",
						SupportEmail: "test@gmail.com",
					}, nil).Times(1)
				s.EXPECT().FindSubscriptionByID(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&datastore.Subscription{
						Status: datastore.PendingSubscriptionStatus,
					}, nil)

				m.EXPECT().
					FindEventDeliveryByID(gomock.Any(), gomock.Any()).
					Return(&datastore.EventDelivery{
						Status: datastore.ScheduledEventStatus,
						Metadata: &datastore.Metadata{
							Data:            []byte(`{"event": "invoice.completed"}`),
							NumTrials:       4,
							RetryLimit:      3,
							IntervalSeconds: 20,
						},
					}, nil).Times(1)

				r.EXPECT().ShouldAllow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				r.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&redis_rate.Result{
					Limit:     redis_rate.PerMinute(10),
					Allowed:   10,
					Remaining: 10,
				}, nil).Times(1)

				m.EXPECT().
					UpdateStatusOfEventDelivery(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				o.EXPECT().
					FetchGroupByID(gomock.Any(), gomock.Any()).
					Return(&datastore.Group{
						LogoURL: "",
						Config: &datastore.GroupConfig{
							Signature: &datastore.SignatureConfiguration{
								Header: config.SignatureHeaderProvider("X-Convoy-Signature"),
								Hash:   "SHA256",
							},
							Strategy: &datastore.StrategyConfiguration{
								Type:       datastore.LinearStrategyProvider,
								Duration:   60,
								RetryCount: 1,
							},
							RateLimit:       &datastore.DefaultRateLimitConfig,
							DisableEndpoint: true,
						},
					}, nil).Times(1)

				s.EXPECT().UpdateSubscriptionStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				m.EXPECT().
					UpdateEventDeliveryWithAttempt(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)

				q.EXPECT().
					Write(convoy.NotificationProcessor, convoy.DefaultQueue, gomock.Any()).
					Return(nil).Times(1)
			},
			nFn: func() func() {
				httpmock.Activate()

				httpmock.RegisterResponder("POST", "https://google.com",
					httpmock.NewStringResponder(200, ``))

				return func() {
					httpmock.DeactivateAndReset()
				}
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			groupRepo := mocks.NewMockGroupRepository(ctrl)
			appRepo := mocks.NewMockApplicationRepository(ctrl)
			msgRepo := mocks.NewMockEventDeliveryRepository(ctrl)
			apiKeyRepo := mocks.NewMockAPIKeyRepository(ctrl)
			userRepo := mocks.NewMockUserRepository(ctrl)
			cache := mocks.NewMockCache(ctrl)
			rateLimiter := mocks.NewMockRateLimiter(ctrl)
			subRepo := mocks.NewMockSubscriptionRepository(ctrl)
			q := mocks.NewMockQueuer(ctrl)

			err := config.LoadConfig(tc.cfgPath)
			if err != nil {
				t.Errorf("Failed to load config file: %v", err)
			}

			cfg, err := config.Get()
			if err != nil {
				t.Errorf("failed to get config: %v", err)
			}

			err = realm_chain.Init(&cfg.Auth, apiKeyRepo, userRepo, cache)
			if err != nil {
				t.Errorf("failed to initialize realm chain : %v", err)
			}

			if tc.nFn != nil {
				deferFn := tc.nFn()
				defer deferFn()
			}

			if tc.dbFn != nil {
				tc.dbFn(appRepo, groupRepo, msgRepo, rateLimiter, subRepo, q)
			}

			processFn := ProcessEventDelivery(appRepo, msgRepo, groupRepo, rateLimiter, subRepo, q)

			payload := json.RawMessage(tc.msg.UID)

			job := queue.Job{
				Payload: payload,
			}

			task := asynq.NewTask(string(convoy.EventProcessor), job.Payload, asynq.Queue(string(convoy.EventQueue)), asynq.ProcessIn(job.Delay))

			err = processFn(context.Background(), task)

			// Assert.
			assert.Equal(t, tc.expectedError, err)
		})
	}
}

func TestProcessEventDeliveryConfig(t *testing.T) {
	tt := []struct {
		name                string
		subscription        *datastore.Subscription
		group               *datastore.Group
		wantRetryConfig     *datastore.StrategyConfiguration
		wantRateLimitConfig *datastore.RateLimitConfiguration
		wantDisableEndpoint bool
	}{
		{
			name: "Subscription Config is primary config",
			subscription: &datastore.Subscription{
				RetryConfig: &datastore.RetryConfiguration{
					Type:       datastore.LinearStrategyProvider,
					Duration:   2,
					RetryCount: 3,
				},
				RateLimitConfig: &datastore.RateLimitConfiguration{
					Count:    100,
					Duration: 1,
				},
				DisableEndpoint: func(b bool) *bool {
					return &b
				}(true),
			},
			group: &datastore.Group{
				Config: &datastore.GroupConfig{
					Strategy:        &datastore.DefaultStrategyConfig,
					RateLimit:       &datastore.DefaultRateLimitConfig,
					DisableEndpoint: false,
				},
			},
			wantRetryConfig: &datastore.StrategyConfiguration{
				Type:       datastore.LinearStrategyProvider,
				Duration:   2,
				RetryCount: 3,
			},
			wantRateLimitConfig: &datastore.RateLimitConfiguration{
				Count:    100,
				Duration: 1,
			},
			wantDisableEndpoint: true,
		},

		{
			name:         "Group Config is primary config",
			subscription: &datastore.Subscription{},
			group: &datastore.Group{
				Config: &datastore.GroupConfig{
					Strategy: &datastore.StrategyConfiguration{
						Type:       datastore.ExponentialStrategyProvider,
						Duration:   3,
						RetryCount: 4,
					},
					RateLimit: &datastore.RateLimitConfiguration{
						Count:    100,
						Duration: 10,
					},
					DisableEndpoint: false,
				},
			},
			wantRetryConfig: &datastore.StrategyConfiguration{
				Type:       datastore.ExponentialStrategyProvider,
				Duration:   3,
				RetryCount: 4,
			},
			wantRateLimitConfig: &datastore.RateLimitConfiguration{
				Count:    100,
				Duration: 10,
			},
			wantDisableEndpoint: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			evConfig := &EventDeliveryConfig{subscription: tc.subscription, group: tc.group}

			if tc.wantRetryConfig != nil {
				rc, err := evConfig.retryConfig()

				assert.Nil(t, err)

				assert.Equal(t, tc.wantRetryConfig.Type, rc.Type)
				assert.Equal(t, tc.wantRetryConfig.Duration, rc.Duration)
				assert.Equal(t, tc.wantRetryConfig.RetryCount, rc.RetryCount)
			}

			if tc.wantRateLimitConfig != nil {
				rlc := evConfig.rateLimitConfig()

				assert.Equal(t, tc.wantRateLimitConfig.Count, rlc.Count)
				assert.Equal(t, tc.wantRateLimitConfig.Duration, rlc.Duration)
			}

			disableEndpoint := evConfig.disableEndpoint()
			assert.Equal(t, tc.wantDisableEndpoint, disableEndpoint)
		})
	}
}
