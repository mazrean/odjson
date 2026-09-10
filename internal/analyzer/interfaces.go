package analyzer

import "go/types"

var (
	errorType     = types.Universe.Lookup("error").Type()
	byteSliceType = types.NewSlice(types.Typ[types.Byte])

	// jsonMarshaler is interface{ MarshalJSON() ([]byte, error) }.
	jsonMarshaler = mustIface("MarshalJSON", nil, []types.Type{byteSliceType, errorType})
	// jsonUnmarshaler is interface{ UnmarshalJSON([]byte) error }.
	jsonUnmarshaler = mustIface("UnmarshalJSON", []types.Type{byteSliceType}, []types.Type{errorType})
	// textMarshaler is interface{ MarshalText() ([]byte, error) }.
	textMarshaler = mustIface("MarshalText", nil, []types.Type{byteSliceType, errorType})
	// textUnmarshaler is interface{ UnmarshalText([]byte) error }.
	textUnmarshaler = mustIface("UnmarshalText", []types.Type{byteSliceType}, []types.Type{errorType})
	// isZeroer is interface{ IsZero() bool }.
	isZeroer = mustIface("IsZero", nil, []types.Type{types.Typ[types.Bool]})
)

func mustIface(name string, params, results []types.Type) *types.Interface {
	tuple := func(ts []types.Type) *types.Tuple {
		vars := make([]*types.Var, len(ts))
		for i, t := range ts {
			vars[i] = types.NewParam(0, nil, "", t)
		}
		return types.NewTuple(vars...)
	}
	sig := types.NewSignatureType(nil, nil, nil, tuple(params), tuple(results), false)
	fn := types.NewFunc(0, nil, name, sig)
	iface := types.NewInterfaceType([]*types.Func{fn}, nil)
	iface.Complete()
	return iface
}

// implements reports whether t and *t satisfy iface.
func implements(t types.Type, iface *types.Interface) (val, ptr bool) {
	val = types.Implements(t, iface)
	if _, isPtr := t.Underlying().(*types.Pointer); isPtr {
		return val, val
	}
	if _, isIface := t.Underlying().(*types.Interface); isIface {
		return val, val
	}
	ptr = types.Implements(types.NewPointer(t), iface)
	return val, ptr
}

// hasOwnMethod reports whether the named type declares a method with the given
// name itself (as opposed to promoting one from an embedded field).
func hasOwnMethod(named *types.Named, name string) bool {
	for method := range named.Methods() {
		if method.Name() == name {
			return true
		}
	}
	return false
}
