package selfupdate

import (
	"fmt"
	"io"
)

// mockRequester used for some mock testing to ensure the requester contract
// works as specified.
type mockRequester struct {
	currentIndex int
	fetches      []func(string) (io.ReadCloser, error)
}

func (mr *mockRequester) handleRequest(requestHandler func(string) (io.ReadCloser, error)) {
	if mr.fetches == nil {
		mr.fetches = []func(string) (io.ReadCloser, error){}
	}
	mr.fetches = append(mr.fetches, requestHandler)
}

func (mr *mockRequester) Fetch(url string) (io.ReadCloser, error) {
	if len(mr.fetches) <= mr.currentIndex {
		return nil, fmt.Errorf("No for currentIndex %d to mock", mr.currentIndex)
	}
	current := mr.fetches[mr.currentIndex]
	mr.currentIndex++

	return current(url)
}
