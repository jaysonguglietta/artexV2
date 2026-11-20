import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GuidedAssessmentForm } from "./guided-assessment-form";

const profiles = [{ id: "1", name: "My provider", model: "my-model", is_default: true }] as Parameters<
  typeof GuidedAssessmentForm
>[0]["profiles"];
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe = vi.fn();
      unobserve = vi.fn();
      disconnect = vi.fn();
    },
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("guided assessment form", () => {
  it("requires a website and explicit authorization, then builds a limited assessment", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    render(<GuidedAssessmentForm profiles={profiles} loading={false} busy={false} onSave={save} />);
    const submit = screen.getByRole("button", { name: "Save and review setup" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Which website would you like to check?"), {
      target: { value: "example.com" },
    });
    expect(submit.disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox"));
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);
    await waitFor(() => expect(save).toHaveBeenCalledOnce());
    expect(save.mock.calls[0][0].description).toContain("Do not brute-force");
    expect(save.mock.calls[0][0].name).toBe("example.com — website check");
    expect(save.mock.calls[0][1]).toBe(1);
  });

  it("shows URL errors without creating an assessment", async () => {
    const save = vi.fn();
    render(<GuidedAssessmentForm profiles={profiles} loading={false} busy={false} onSave={save} />);
    fireEvent.change(screen.getByLabelText("Which website would you like to check?"), {
      target: { value: "http://example.com" },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Save and review setup" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Use an HTTPS website"));
    expect(save).not.toHaveBeenCalled();
  });

  it("links to provider setup when configuration is missing", () => {
    render(<GuidedAssessmentForm profiles={[]} loading={false} busy={false} onSave={vi.fn()} />);
    expect(screen.getByRole("link", { name: "Open AI provider settings" }).getAttribute("href")).toBe("/system/llm");
    expect((screen.getByRole("button", { name: "Save and review setup" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
