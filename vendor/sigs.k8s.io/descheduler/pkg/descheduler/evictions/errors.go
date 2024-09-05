package evictions

type EvictionNodeLimitError struct {
	node string
}

func (e EvictionNodeLimitError) Error() string {
	return "maximum number of evicted pods per node reached"
}

func NewEvictionNodeLimitError(node string) *EvictionNodeLimitError {
	return &EvictionNodeLimitError{
		node: node,
	}
}

var _ error = &EvictionNodeLimitError{}

type EvictionNamespaceLimitError struct {
	namespace string
}

func (e EvictionNamespaceLimitError) Error() string {
	return "maximum number of evicted pods per namespace reached"
}

func NewEvictionNamespaceLimitError(namespace string) *EvictionNamespaceLimitError {
	return &EvictionNamespaceLimitError{
		namespace: namespace,
	}
}

var _ error = &EvictionNamespaceLimitError{}

type EvictionTotalLimitError struct{}

func (e EvictionTotalLimitError) Error() string {
	return "maximum number of evicted pods per a descheduling cycle reached"
}

func NewEvictionTotalLimitError() *EvictionTotalLimitError {
	return &EvictionTotalLimitError{}
}

var _ error = &EvictionTotalLimitError{}
