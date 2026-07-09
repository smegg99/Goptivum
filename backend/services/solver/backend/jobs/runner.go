// jobs/runner.go

package jobs

import (
	"context"
	"errors"
	"io"
	"log"

	"google.golang.org/protobuf/encoding/protojson"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

var marshaler = protojson.MarshalOptions{EmitUnpopulated: true}

// Run streams solver updates into the job until the stream ends. It only
// forwards payloads and tracks process state — no solver semantics.
func Run(ctx context.Context, client arrangov1.SolverServiceClient,
	req *arrangov1.SolveRequest, job *Job) {
	stream, err := client.Solve(ctx, req)
	if err != nil {
		log.Printf("job %s: solve call failed: %v", job.ID, err)
		job.Finish(StateError)
		return
	}
	for {
		update, err := stream.Recv()
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				job.Finish(StateDone)
			case ctx.Err() != nil:
				job.Finish(StateCancelled)
			default:
				log.Printf("job %s: stream error: %v", job.ID, err)
				job.Finish(StateError)
			}
			return
		}
		payload, err := marshaler.Marshal(update)
		if err != nil {
			log.Printf("job %s: marshal update: %v", job.ID, err)
			continue
		}
		job.Publish(payload)
	}
}
