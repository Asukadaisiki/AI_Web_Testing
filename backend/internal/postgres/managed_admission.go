package postgres

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/jackc/pgx/v5"
)

func validateManagedRunTx(ctx context.Context, tx pgx.Tx, run domain.Run) (runSpecification, error) {
	var spec runSpecification
	if err := json.Unmarshal(run.Specification, &spec); err != nil {
		return spec, ErrConflict
	}
	command := spec.Command
	if err := command.ValidateManagedExecution(); err != nil {
		return spec, ErrConflict
	}
	if command.RunID != run.ID || command.ProjectID != run.ProjectID || command.ConversationID != run.ConversationID ||
		!reflect.DeepEqual(spec.Execution, command.Execution) || len(command.Operations) != 0 ||
		command.WorkerURL != "" || command.FencingToken != 0 {
		return spec, ErrConflict
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT v.snapshot FROM harness_versions v JOIN project_harnesses h ON h.id=v.harness_id
		WHERE v.id=$1 AND h.project_id=$2`, command.Execution.Harness.VersionID, run.ProjectID).Scan(&raw); err != nil {
		if err == pgx.ErrNoRows {
			return spec, ErrConflict
		}
		return spec, err
	}
	var pinned execution.HarnessSnapshot
	if err := json.Unmarshal(raw, &pinned); err != nil || !reflect.DeepEqual(pinned, command.Execution.Harness) {
		return spec, ErrConflict
	}
	// Revocation blocks new selection, not an already pinned managed command.
	return spec, nil
}
