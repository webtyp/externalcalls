package externalcalls

import "webtyp.com/model"

// QueuedCallModel is the table of calls to other systems that are waiting, done or failed.
var QueuedCallModel = model.Definition{
	Name: "queued_call",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "tenant_id", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "module", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "entity", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "record_id", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "call", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "mode", Type: model.Int(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "state", Type: model.Text(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "attempts", Type: model.Int(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "next_attempt_at", Type: model.NanoTime(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "last_error", Type: model.Text(), DB: &model.FieldDB{}},
		{Name: "created_at", Type: model.NanoTime(), NotNull: true, DB: &model.FieldDB{}},
		{Name: "updated_at", Type: model.NanoTime(), NotNull: true, DB: &model.FieldDB{}},
	},
}
