"use client";
import type { ReactNode } from "react";

import { IntlProvider } from "use-intl";

import messages from "../../messages/en.json";

const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;
export function IntlRuntimeProvider({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <IntlProvider locale="en" messages={messages} timeZone={TIME_ZONE}>
      {children}
    </IntlProvider>
  );
}
