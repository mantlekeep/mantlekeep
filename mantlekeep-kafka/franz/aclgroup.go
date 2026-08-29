package franz

import (
	"fmt"

	kafkagrant "github.com/mantlekeep/mantlekeep/mantlekeep-kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// aclGroupKey is everything about a binding EXCEPT its operation. Bindings sharing a key
// can be written in one request with a combined operation set.
type aclGroupKey struct {
	principal  kafkagrant.Principal
	host       string
	resource   kmsg.ACLResourceType
	pattern    kmsg.ACLResourcePatternType
	permission kmsg.ACLPermissionType
}

type aclGroup struct {
	key        aclGroupKey
	names      []string
	operations []kmsg.ACLOperation
}

// groupBindings collapses bindings into per-key groups, preserving input order so a
// request — and any error naming it — is reproducible rather than map-ordered.
func groupBindings(bindings []kafkagrant.ACLBinding) []*aclGroup {
	var ordered []*aclGroup
	index := make(map[aclGroupKey]*aclGroup, len(bindings))
	for _, binding := range bindings {
		key := aclGroupKey{
			principal:  binding.Principal,
			host:       binding.Host,
			resource:   binding.ResourceType,
			pattern:    binding.Pattern,
			permission: binding.Permission,
		}
		group, seen := index[key]
		if !seen {
			group = &aclGroup{key: key}
			index[key] = group
			ordered = append(ordered, group)
		}
		group.addName(binding.ResourceName)
		group.addOperation(binding.Operation)
	}
	return ordered
}

func (g *aclGroup) addName(name string) {
	for _, existing := range g.names {
		if existing == name {
			return
		}
	}
	g.names = append(g.names, name)
}

func (g *aclGroup) addOperation(operation kmsg.ACLOperation) {
	for _, existing := range g.operations {
		if existing == operation {
			return
		}
	}
	g.operations = append(g.operations, operation)
}

// builder turns the group into a kadm ACL builder.
//
// It refuses anything this module does not write. Resource types beyond topic and group,
// and DENY permissions, are not part of the namespace model; a binding asking for one
// arrived from somewhere that has not thought about what it means here, and translating
// it silently would apply a rule nobody designed.
func (g *aclGroup) builder() (*kadm.ACLBuilder, error) {
	if g.key.permission != kmsg.ACLPermissionTypeAllow {
		return nil, fmt.Errorf("franz: refusing to write a %s binding — this adapter writes ALLOW bindings only", g.key.permission)
	}
	if g.key.pattern != kmsg.ACLResourcePatternTypePrefixed && g.key.pattern != kmsg.ACLResourcePatternTypeLiteral {
		return nil, fmt.Errorf("franz: resource pattern %s cannot be created; only LITERAL and PREFIXED can", g.key.pattern)
	}

	builder := kadm.NewACLs().
		ResourcePatternType(g.key.pattern).
		Operations(g.operations...).
		Allow(string(g.key.principal)).
		AllowHosts(g.key.host)

	switch g.key.resource {
	case kmsg.ACLResourceTypeTopic:
		builder = builder.Topics(g.names...)
	case kmsg.ACLResourceTypeGroup:
		builder = builder.Groups(g.names...)
	default:
		return nil, fmt.Errorf("franz: resource type %s is not part of the namespace model (topic and group only)", g.key.resource)
	}
	return builder, nil
}
