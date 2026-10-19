package domain

import "context"

// HoldHashSlots fills every slot of s's hash limiter with a hash that blocks
// until the returned func is called, and returns once all of them are held.
func HoldHashSlots(s *Service) (release func()) {
	n := cap(s.hashes.slots)
	held, freed := make(chan struct{}), make(chan struct{})
	for range n {
		go func() {
			_ = s.hashes.run(context.Background(), func() {
				held <- struct{}{}
				<-freed
			})
		}()
	}
	for range n {
		<-held
	}
	return func() { close(freed) }
}
