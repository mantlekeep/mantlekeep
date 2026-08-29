package franz

import (
	"context"
	"fmt"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// CreateACLs writes the bindings to the cluster.
//
// The bindings are grouped by everything except the operation, so one request carries
// all the operations for a resource instead of one request per operation. kadm's builder
// applies its operation set to every resource in it, which is why topic bindings and
// group bindings — whose operation sets differ — go in separate groups.
func (a *Admin) CreateACLs(ctx context.Context, bindings []kafkagrant.ACLBinding) error {
	for _, group := range groupBindings(bindings) {
		builder, err := group.builder()
		if err != nil {
			return err
		}
		results, err := a.client.CreateACLs(ctx, builder)
		if err != nil {
			return err
		}
		for _, result := range results {
			if result.Err != nil {
				return fmt.Errorf("create acl %s on %s %q: %w (%s)",
					result.Operation, result.Type, result.Name, result.Err, result.ErrMessage)
			}
		}
	}
	return nil
}

// DescribeACLs reads back what the cluster actually holds for principal on resourceName
// with the given pattern — both ALLOW and DENY bindings, because the artifact reports
// what the cluster permits, not what this adapter wrote.
func (a *Admin) DescribeACLs(
	ctx context.Context,
	principal kafkagrant.Principal,
	resourceName string,
	pattern kmsg.ACLResourcePatternType,
) ([]kafkagrant.ACLBinding, error) {
	filter := kadm.NewACLs().
		Topics(resourceName).
		Groups(resourceName).
		ResourcePatternType(pattern).
		Operations(). // no operations given → match ANY operation
		Allow(string(principal)).
		AllowHosts().
		Deny(string(principal)).
		DenyHosts()

	results, err := a.client.DescribeACLs(ctx, filter)
	if err != nil {
		return nil, err
	}
	var bindings []kafkagrant.ACLBinding
	for _, result := range results {
		if result.Err != nil {
			return nil, fmt.Errorf("describe acls for %s: %w (%s)", principal, result.Err, result.ErrMessage)
		}
		for _, described := range result.Described {
			bindings = append(bindings, kafkagrant.ACLBinding{
				Principal:    kafkagrant.Principal(described.Principal),
				Host:         described.Host,
				ResourceType: described.Type,
				ResourceName: described.Name,
				Pattern:      described.Pattern,
				Operation:    described.Operation,
				Permission:   described.Permission,
			})
		}
	}
	return bindings, nil
}
