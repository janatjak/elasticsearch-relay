package main

import (
	"github.com/google/uuid"
	"net/http"
	"runtime"
	"sync"
)

type queuenode struct {
	data interface{}
	next *queuenode
}

// A go-routine safe FIFO (first in first out) data stucture.
type Queue struct {
	head       *queuenode
	tail       *queuenode
	count      int
	totalCount uint64
	lock       *sync.Mutex
}

// Creates a new pointer to a new queue.
func NewQueue() *Queue {
	q := &Queue{}
	q.lock = &sync.Mutex{}
	return q
}

func (q *Queue) Push(uuid uuid.UUID, req *http.Request, body []byte) {
	x := &RelayRequest{
		Uuid:    uuid,
		Method:  req.Method,
		Url:     req.RequestURI,
		Headers: req.Header.Clone(),
		Body:    body,
		Retries: 0,
	}

	q.push(x)
}

func (q *Queue) RePush(relayRequest *RelayRequest) {
	q.push(relayRequest)
}

func (q *Queue) Get() *RelayRequest {
	item := q.pull()
	if item != nil {
		return item.(*RelayRequest)
	}

	return nil
}

func (q *Queue) Clean() {
	for q.count > 0 {
		q.pull()
	}

	runtime.GC()
}

// Returns the number of elements in the queue (i.e. size/length)
// go-routine safe.
func (q *Queue) Len() int {
	q.lock.Lock()
	defer q.lock.Unlock()
	return q.count
}

func (q *Queue) TotalCount() uint64 {
	return q.totalCount
}

// Pushes/inserts a value at the end/tail of the queue.
// Note: this function does mutate the queue.
// go-routine safe.
func (q *Queue) push(item interface{}) {
	q.lock.Lock()
	defer q.lock.Unlock()

	n := &queuenode{data: item}

	if q.tail == nil {
		q.tail = n
		q.head = n
	} else {
		q.tail.next = n
		q.tail = n
	}
	q.count++
	q.totalCount++
}

// Returns the value at the front of the queue.
// i.e. the oldest value in the queue.
// Note: this function does mutate the queue.
// go-routine safe.
func (q *Queue) pull() interface{} {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.head == nil {
		return nil
	}

	n := q.head
	q.head = n.next

	if q.head == nil {
		q.tail = nil
	}
	q.count--

	return n.data
}
