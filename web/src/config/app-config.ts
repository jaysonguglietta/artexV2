import packageJson from "../../package.json";
export const APP_CONFIG = {
  name: "ARTEX",
  version: packageJson.version,
  copyright: `© ${new Date().getFullYear()}, ARTEX.`,
  meta: {
    title: "ARTEX: Authorized Assessment Console",
    description: "A local console for authorized security assessments",
  },
};
