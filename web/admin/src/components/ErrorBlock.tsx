// Back-compat re-export. New pages should import ErrorState directly from
// @authman/shared; this shim lets the existing call sites compile during the
// gradual refactor.
export { ErrorState as ErrorBlock } from "@authman/shared";
