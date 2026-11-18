import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { IntlProvider } from "use-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import english from "../../../../../../messages/en.json";
import { UpdateCard } from "./update-card";

const requests = vi.hoisted(() => ({ checkUpdate: vi.fn(), applyUpdate: vi.fn(), rollbackUpdate: vi.fn() }));
const notifications = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("@/lib/api", () => ({ api: requests, sseUrl: (path: string) => path }));
vi.mock("sonner", () => ({ toast: notifications }));

const release = {
  current: "v1.0.0",
  latest: "v1.1.0",
  has_update: true,
  has_backup: true,
  asset_available: true,
  mode: "docker",
  os: "linux",
  arch: "arm64",
  repo: "jaysonguglietta/artexV2",
  comparable: true,
  html_url: "https://github.com/jaysonguglietta/artexV2/releases/tag/v1.1.0",
};

function show() {
  return render(
    <IntlProvider locale="en" messages={english}>
      <UpdateCard />
    </IntlProvider>,
  );
}

let clipboardDescriptor: PropertyDescriptor | undefined;

beforeEach(() => {
  clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");
  vi.resetAllMocks();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  if (clipboardDescriptor) Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else Reflect.deleteProperty(navigator, "clipboard");
});

describe("read-only release installation", () => {
  it.each([false, undefined])("never applies or rolls back when the capability is %s", async (capability) => {
    requests.checkUpdate.mockResolvedValue({ ...release, in_process_updates: capability });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const stream = vi.fn();
    vi.stubGlobal("EventSource", stream);
    const copy = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: copy } });
    show();

    await screen.findByText("v1.0.0");
    expect(screen.getByText("Installation happens outside this application")).toBeVisible();
    expect(screen.queryByRole("button", { name: /^Update (to|now)/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Roll back/ })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Verified installation guide" })).toHaveAttribute(
      "href",
      "https://github.com/jaysonguglietta/artexV2/blob/main/docs/SIGNED-RELEASES.md",
    );
    expect(screen.getByRole("link", { name: "Changelog" })).toHaveAttribute("href", release.html_url);

    fireEvent.click(screen.getByRole("button", { name: "Check for updates" }));
    await waitFor(() => expect(requests.checkUpdate).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByRole("button", { name: "Copy current version" }));
    await waitFor(() => expect(copy).toHaveBeenCalledWith("v1.0.0"));
    expect(requests.applyUpdate).not.toHaveBeenCalled();
    expect(requests.rollbackUpdate).not.toHaveBeenCalled();
    expect(confirm).not.toHaveBeenCalled();
    expect(stream).not.toHaveBeenCalled();
  });

  it("gives recovery guidance when copying the version is denied", async () => {
    requests.checkUpdate.mockResolvedValue({ ...release, in_process_updates: false });
    const copy = vi.fn().mockRejectedValue(new Error("clipboard permission denied"));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: copy } });
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Copy current version" }));
    await waitFor(() => expect(notifications.error).toHaveBeenCalledWith(english.settingsPage.update.toast.copyFailed));
    expect(requests.applyUpdate).not.toHaveBeenCalled();
    expect(requests.rollbackUpdate).not.toHaveBeenCalled();
  });
});
