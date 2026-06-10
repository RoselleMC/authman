import { useLocation, type Location, type NavigateFunction, type To } from "react-router-dom";

type BackState = {
  backTo?: string;
};

function locationPath(location: Location): string {
  return `${location.pathname}${location.search}${location.hash}`;
}

export function currentBackTarget(location: Location): string {
  const state = location.state as BackState | null;
  return state?.backTo || locationPath(location);
}

export function navigateWithBack(navigate: NavigateFunction, to: To, location: Location) {
  navigate(to, { state: { backTo: locationPath(location) } });
}

export function useBackTarget(fallback: string) {
  const location = useLocation();
  const state = location.state as BackState | null;
  return state?.backTo || fallback;
}
