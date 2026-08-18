package emit

import "fmt"

// The generic mappers, and why every DTO this generator writes opts into them.
//
// The framework's default posture is that every mapping seat is hand-written —
// ToCommand, ApplyTo, FromEntity, ToCriteria, FromResult — and the two generic
// helpers are an OFFER a DTO has to accept by embedding a marker. The offer is
// worth accepting exactly when the two shapes are name-aligned, and everything
// this generator emits is: there is no key in the language that renames a field
// on one side only. `fields[].name` drives the Go name and the wire name
// together; `parts[].as` renames both halves of a composite at once.
//
// So the marker is not a shortcut here, it is a statement of fact — and it buys
// a check the generator cannot give itself. With it, the route constructors
// validate the pair at Mount: a Request field with nowhere to land, or a
// Response field with no Result behind it, is a boot panic naming the field.
// That is a regression net over the EMITTERS. Before it, two emitters drifting
// apart shipped a silent null.
//
// The rule reads by layer, and it is why the check is one-way on each side: a
// type in web/ (Request, Response) must be FULLY connected, because a wire
// field with no counterpart either renders null forever or has its value
// dropped in silence. A type in application/ (Command, Result) may carry more —
// the path id, an identity overlay, a Result field a Response deliberately cuts
// off the wire.

const (
	autoRequestEmbed  = "fwrequests.Auto"
	autoResponseEmbed = "fwresponses.Auto"
)

// autoDoc is the one-line reason the marker is on a DTO, written where a reader
// meets it rather than in a manual they would have to go find.
func autoDoc(kind string) string {
	if kind == "request" {
		return "The embedded marker opts this Request into the framework's generic " +
			"Request→Command mapping: every field travels to the same-named Command field, " +
			"and the pair is checked at boot. A shape that needed renaming or reshaping " +
			"would drop the marker and write ToCommand by hand instead."
	}
	return "The embedded marker opts this Response into the framework's generic " +
		"Result→Response mapping: every field is read from the same-named Result field, " +
		"and the pair is checked at boot. A shape that needed renaming or reshaping " +
		"would drop the marker and write FromResult by hand instead."
}

// emitAutoToCommand writes a Request's ToCommand as the generic call.
func emitAutoToCommand(s *src, requestType, commandType string) {
	s.Doc("ToCommand hands the body to the application layer unchanged. "+
		"No normalisation happens here: the domain is what decides a value's final form.",
		"", autoDoc("request"))
	s.L("func (r %s) ToCommand() *commands.%s {", requestType, commandType)
	s.L("\treturn fwrequests.AutoFromRequest[*commands.%s](r)", commandType)
	s.L("}")
}

// emitAutoFromResult writes a Response's FromResult as the generic call.
//
// resultRef is the Result type as this file must spell it — qualified by the
// package it lives in, which differs between the write side (commands) and the
// read side (appqueries).
func emitAutoFromResult(s *src, responseType, resultRef string, extraDoc ...string) {
	doc := append([]string{
		fmt.Sprintf("FromResult projects the application Result onto %s.", responseType),
	}, extraDoc...)
	doc = append(doc, "", autoDoc("response"))
	s.Doc(doc...)
	s.L("func (%s) FromResult(r %s) %s {", responseType, resultRef, responseType)
	s.L("\treturn fwresponses.AutoFromResult[%s](r)", responseType)
	s.L("}")
}
