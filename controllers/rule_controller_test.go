package controllers

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const delay = time.Millisecond

func TestRetryOnError(t *testing.T) {

	var cnt, attempts int

	var fallbackCalled bool
	var dieFunc = func() { fallbackCalled = true }

	var createMapFunc func() error

	t.Run("should retry on error", func(t *testing.T) {

		//Given
		cnt, attempts = 0, 2
		fallbackCalled = false

		createMapFunc = func() error {
			cnt++
			if cnt == 1 {
				return errors.New("error only on first invocation")
			}
			return nil
		}

		//when
		retryOnError(createMapFunc, attempts, delay).or(dieFunc)

		//then
		assert.Equal(t, attempts, cnt)
		assert.False(t, fallbackCalled)
	})

	t.Run("should not call a callback function if last attempt is successful", func(t *testing.T) {

		//Given
		cnt, attempts = 0, 5

		createMapFunc = func() error {
			cnt++
			if cnt < 5 {
				return errors.New(fmt.Sprintf("error only on first four invocations (current: %d)", cnt))
			}
			return nil
		}

		//when
		retryOnError(createMapFunc, attempts, delay).or(dieFunc)

		//then
		assert.Equal(t, attempts, cnt)
		assert.False(t, fallbackCalled)
	})

	t.Run("should call a callback function after five failed attempts", func(t *testing.T) {

		//Given
		cnt, attempts = 0, 2

		createMapFunc = func() error {
			cnt++
			return errors.New(fmt.Sprintf("error on every invocation (current: %d)", cnt))
		}

		//when
		retryOnError(createMapFunc, attempts, delay).or(dieFunc)

		//then
		assert.Equal(t, attempts, cnt)
		assert.True(t, fallbackCalled)
	})
}