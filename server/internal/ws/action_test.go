package ws

import "testing"

func TestActionResultMustMatchConnectionAndExplicitSuccess(t *testing.T) {
	owner := &client{send: make(chan []byte, 4)}
	stranger := &client{send: make(chan []byte, 4)}
	result := make(chan actionResult, 1)
	h := &Hub{actions: map[string]pendingAction{"a": {client: owner, result: result}}}
	h.actionResult(stranger, []byte(`{"type":"action_result","action_id":"a","success":true}`))
	if len(result) != 0 {
		t.Fatal("accepted another connection")
	}
	h.actionResult(owner, []byte(`{"type":"action_result","action_id":"a"}`))
	if len(result) != 0 {
		t.Fatal("accepted missing result status")
	}
	h.actionResult(owner, []byte(`{"type":"action_result","action_id":"a","success":true}`))
	if got := <-result; !got.Success {
		t.Fatal("result not delivered")
	}
}
