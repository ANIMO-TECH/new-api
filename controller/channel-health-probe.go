package controller

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type channelHealthProbeSummary struct {
	Tested    int `json:"tested"`
	Recovered int `json:"recovered"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Exhausted int `json:"exhausted"`
}

type channelHealthProbeResult struct {
	recovered bool
	failed    bool
	skipped   bool
	exhausted bool
}

func runChannelHealthProbeTask(ctx context.Context, report func(processed, total int)) (channelHealthProbeSummary, error) {
	setting := operation_setting.GetHealthProbeSetting()
	if err := model.EnsureAutoDisabledChannelHealthProbes(int64(setting.InitialDelaySeconds)); err != nil {
		return channelHealthProbeSummary{}, err
	}
	states, err := model.ListDueChannelHealthProbes(common.GetTimestamp(), setting.Concurrency*100)
	if err != nil {
		return channelHealthProbeSummary{}, err
	}
	if len(states) == 0 {
		if report != nil {
			report(0, 0)
		}
		return channelHealthProbeSummary{}, nil
	}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return channelHealthProbeSummary{}, err
	}

	jobs := make(chan *model.ChannelHealthProbeState)
	results := make(chan channelHealthProbeResult)
	workerCount := setting.Concurrency
	if workerCount > len(states) {
		workerCount = len(states)
	}
	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for state := range jobs {
				results <- executeChannelHealthProbe(ctx, state, testUserID, setting)
			}
		}()
	}
	go func() {
		defer close(results)
		for _, state := range states {
			select {
			case <-ctx.Done():
				close(jobs)
				workers.Wait()
				return
			case jobs <- state:
			}
		}
		close(jobs)
		workers.Wait()
	}()

	summary := channelHealthProbeSummary{}
	processed := 0
	for result := range results {
		processed++
		summary.Tested++
		if result.recovered {
			summary.Recovered++
		}
		if result.failed {
			summary.Failed++
		}
		if result.skipped {
			summary.Skipped++
		}
		if result.exhausted {
			summary.Exhausted++
		}
		if report != nil {
			report(processed, len(states))
		}
	}
	return summary, nil
}

func executeChannelHealthProbe(parent context.Context, state *model.ChannelHealthProbeState, testUserID int, setting operation_setting.HealthProbeSetting) channelHealthProbeResult {
	current, err := model.IsCurrentChannelHealthProbeState(state)
	if err != nil || !current {
		return channelHealthProbeResult{skipped: true}
	}
	channel, key, eligible, err := model.ResolveChannelHealthProbeTarget(state)
	if err != nil || !eligible {
		_, _ = model.RemoveChannelHealthProbeState(state)
		return channelHealthProbeResult{skipped: true}
	}

	probeChannel := *channel
	probeChannel.Key = key
	probeChannel.Keys = nil
	probeChannel.ChannelInfo = model.ChannelInfo{}
	probeContext, cancel := context.WithTimeout(parent, time.Duration(setting.RequestTimeoutSeconds)*time.Second)
	result := testChannel(probeContext, &probeChannel, testUserID, "", "", false)
	cancel()

	current, err = model.IsCurrentChannelHealthProbeState(state)
	if err != nil || !current {
		return channelHealthProbeResult{skipped: true}
	}
	_, _, eligible, err = model.ResolveChannelHealthProbeTarget(state)
	if err != nil || !eligible {
		_, _ = model.RemoveChannelHealthProbeState(state)
		return channelHealthProbeResult{skipped: true}
	}
	if result.localErr == nil && result.newAPIError == nil {
		if !model.UpdateChannelStatus(channel.Id, key, common.ChannelStatusEnabled, "Advoo health probe passed") {
			return channelHealthProbeResult{skipped: true}
		}
		_, _ = model.RemoveChannelHealthProbeState(state)
		if setting.NotifyOnRecovery {
			subject := fmt.Sprintf("通道「%s」（#%d）已由 Advoo 探活恢复", channel.Name, channel.Id)
			service.NotifyRootUser(fmt.Sprintf("advoo_health_probe_recovered_%d", channel.Id), subject, subject)
		}
		return channelHealthProbeResult{recovered: true}
	}

	attempts := state.ProbeAttempts + 1
	exhausted := setting.MaxAttempts > 0 && attempts >= setting.MaxAttempts
	nextProbeAt := int64(0)
	if !exhausted {
		delay := float64(setting.InitialDelaySeconds) * math.Pow(setting.BackoffMultiplier, float64(state.ConsecutiveFailures+1))
		if delay > float64(setting.MaxBackoffSeconds) {
			delay = float64(setting.MaxBackoffSeconds)
		}
		nextProbeAt = common.GetTimestamp() + int64(math.Round(delay))
	}
	errorCode := ""
	errorMessage := "health probe failed"
	if result.newAPIError != nil {
		errorCode = string(result.newAPIError.GetErrorCode())
		errorMessage = result.newAPIError.Error()
	} else if result.localErr != nil {
		errorMessage = result.localErr.Error()
	}
	updated, _ := model.RecordChannelHealthProbeFailure(state, nextProbeAt, errorCode, errorMessage)
	if exhausted && updated && setting.NotifyOnExhausted {
		subject := fmt.Sprintf("通道「%s」（#%d）Advoo 探活已达到最大尝试次数", channel.Name, channel.Id)
		service.NotifyRootUser(fmt.Sprintf("advoo_health_probe_exhausted_%d", channel.Id), subject, subject)
	}
	return channelHealthProbeResult{failed: true, exhausted: exhausted}
}
