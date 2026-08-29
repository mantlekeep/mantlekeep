package franz

import (
	"context"
	"fmt"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterClientQuota sets the principal's quota values on the cluster.
func (a *Admin) AlterClientQuota(ctx context.Context, alteration kafkagrant.QuotaAlteration) error {
	name := alteration.EntityName
	entry := kadm.AlterClientQuotaEntry{
		Entity: kadm.ClientQuotaEntity{{Type: alteration.EntityType, Name: &name}},
	}
	for _, value := range alteration.Values {
		entry.Ops = append(entry.Ops, kadm.AlterClientQuotaOp{Key: value.Key, Value: value.Value})
	}

	results, err := a.client.AlterClientQuotas(ctx, []kadm.AlterClientQuotaEntry{entry})
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Err != nil {
			return fmt.Errorf("alter quota for %s: %w (%s)", result.Entity, result.Err, result.ErrMessage)
		}
	}
	return nil
}

// DescribeClientQuota reads the quota values the cluster actually holds for the entity.
//
// The match is EXACT and strict: a default-tier quota that happens to apply to this
// principal is not the same fact as a quota set ON it, and reporting the former as the
// latter would claim a floor that disappears the moment the default changes.
func (a *Admin) DescribeClientQuota(ctx context.Context, entityType, entityName string) ([]kafkagrant.QuotaValue, error) {
	name := entityName
	component := kadm.DescribeClientQuotaComponent{
		Type:      entityType,
		MatchName: &name,
		MatchType: kmsg.QuotasMatchTypeExact,
	}
	described, err := a.client.DescribeClientQuotas(ctx, true, []kadm.DescribeClientQuotaComponent{component})
	if err != nil {
		return nil, err
	}
	var values []kafkagrant.QuotaValue
	for _, quota := range described {
		for _, value := range quota.Values {
			values = append(values, kafkagrant.QuotaValue{Key: value.Key, Value: value.Value})
		}
	}
	return values, nil
}
