//go:build !unix && !windows

package localpty

const supported = false

func start(Options) (impl, error) {
	return nil, ErrUnsupported
}
