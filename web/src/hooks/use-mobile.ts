import * as React from "react";

const MOBILE_BREAKPOINT = 768;
// An expanded sidebar can overlap tables/cards at tablet widths, 768–1279px.
// Below this breakpoint collapse to icons to preserve content width.
export const TABLET_BREAKPOINT = 1280;

function useBreakpoint(breakpoint: number) {
  const [below, setBelow] = React.useState<boolean | undefined>(undefined);

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`);
    const onChange = () => setBelow(window.innerWidth < breakpoint);
    mql.addEventListener("change", onChange);
    setBelow(window.innerWidth < breakpoint);
    return () => mql.removeEventListener("change", onChange);
  }, [breakpoint]);

  return below;
}

export function useIsMobile() {
  return !!useBreakpoint(MOBILE_BREAKPOINT);
}

// Return whether the viewport is tablet-sized; initial measurement remains undefined,
// so callers can distinguish unknown from false.
export function useIsTablet() {
  return useBreakpoint(TABLET_BREAKPOINT);
}
