// Package engine adapts the GoMud engine to the weather module's pure core. It
// is the ONLY package in the module permitted to import the GoMud engine
// (internal/*): it implements crawler.WorldReader over internal/rooms and
// provides the on-disk graph-cache codec. Keeping all engine calls here is what
// makes the module portable across GoMud and DOGMud.
package engine
