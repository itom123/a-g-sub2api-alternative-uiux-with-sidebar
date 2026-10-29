package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PaymentSession groups installment orders for one subscription purchase.
type PaymentSession struct {
	ent.Schema
}

func (PaymentSession) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_sessions"}}
}

func (PaymentSession) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("plan_id"),
		field.String("plan_name").Default(""),
		field.JSON("installment_schedule_cents", []int64{}).Optional(),
		field.Int64("subscription_group_id"),
		field.Int("subscription_days"),
		field.Float("base_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Int64("allocation_order_id").Optional().Nillable(),
		field.Float("target_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Float("paid_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).Default(0),
		field.String("currency").MaxLen(8).Default("CNY"),
		field.String("status").MaxLen(30).Default("PENDING"),
		field.String("allocation_error").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("allocated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PaymentSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "status"),
		index.Fields("user_id", "plan_id", "status"),
	}
}
