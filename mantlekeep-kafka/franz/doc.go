// Package franz implements kafkagrant.Admin against a real Kafka cluster using
// franz-go's admin client (kadm).
//
// It is the only package in this module that talks to a broker, and it is a SEPARATE
// package for that reason: every rule in kafkagrant is decided without importing a
// client, so every rule is testable without one. Swapping the client library is a change
// confined to this package.
//
// This package does no policy and makes no choices. It translates kafkagrant's value
// types into admin requests, and translates broker errors back into kafkagrant's
// sentinels — most importantly the broker's TOPIC_ALREADY_EXISTS into
// kafkagrant.ErrTopicExists, which is what makes provisioning idempotent.
//
// The caller supplies the client, so this package never owns connection, TLS or SASL
// configuration — those belong to the host, which is also where the credentials the door
// brokered arrive:
//
//	client, err := kgo.NewClient(kgo.SeedBrokers(seeds...) /* TLS, SASL, … */)
//	admin := franz.New(kadm.NewClient(client))
//	adapter := kafkagrant.NewAdapter(admin)
package franz
