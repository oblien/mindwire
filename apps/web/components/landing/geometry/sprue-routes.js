/** Reference connector paths, separated from markup and presentation styles. */
export const customizationConnect = {
  routes: [
    {
      from: { win: "custom-direction", side: "bottom" },
      to: { win: "connect-code", side: "top", at: 0.65 },
      media: "(width >= 1280px)",
    },
    {
      from: { win: "custom-direction", side: "bottom" },
      to: { win: "connect-intro", side: "top" },
      media: "(width < 1280px)",
    },
  ],
};

export const customizationIntro = {
  routes: [
    {
      from: { x: "50%", y: 0 },
      steps: [
        ["s", 32],
        ["w", { to: { win: "custom-heading", side: "top" } }],
        ["s", { to: { win: "custom-heading", side: "top" } }],
      ],
      media: "(width > 600px)",
    },
    {
      from: { x: "50%", y: 0 },
      steps: [["s", { to: { win: "custom-heading", side: "top" } }]],
      media: "(width <= 600px)",
    },
  ],
};

export const customization = {
  routes: [
    {
      from: { win: "custom-brand", side: "right", at: 0.5 },
      to: { win: "custom-tools", side: "left", at: 0.5 },
      media: "(width > 1100px)",
    },
    {
      from: { win: "custom-tools", side: "right", at: 0.5 },
      to: { win: "custom-direction", side: "left", at: 0.5 },
      media: "(width > 1100px)",
    },
  ],
  mobileRoutes: [],
};

export const cloudCapabilities = {
  routes: [
    {
      from: { win: "capability-menu", side: "right", at: 0.55 },
      to: { win: "capability-detail", side: "left", at: 0.5 },
      media: "(width > 900px)",
    },
    {
      from: { win: "capability-menu", side: "bottom", at: 0.7 },
      to: { win: "capability-detail", side: "top", at: 0.5 },
      media: "(width <= 900px)",
    },
  ],
  mobileRoutes: [
    {
      from: { win: "capability-menu", side: "bottom", at: 0.7 },
      to: { win: "capability-detail", side: "top", at: 0.5 },
    },
  ],
};

export const exampleConnection = {
  routes: [
    {
      from: { win: "account-source", side: "right", at: 0.65 },
      to: { win: "account-product", side: "left", at: 0.35 },
    },
  ],
  mobileRoutes: [
    {
      from: { win: "account-source", side: "bottom", at: 0.7 },
      to: { win: "account-product", side: "top", at: 0.3 },
    },
  ],
};

export const businessExample = {
  routes: [
    {
      from: { win: "business-brief", side: "right", at: 0.68 },
      to: { win: "business-product", side: "left", at: 0.32 },
      media: "(width > 1000px)",
    },
    {
      from: { win: "business-brief", side: "bottom", at: 0.8 },
      to: { win: "business-product", side: "top", at: 0.3 },
      media: "(width <= 1000px)",
    },
  ],
  mobileRoutes: [
    {
      from: { win: "business-brief", side: "bottom", at: 0.7 },
      to: { win: "business-product", side: "top", at: 0.3 },
    },
  ],
};

export const sdkExperience = {
  routes: [
    {
      from: { win: "sdk-request", side: "bottom" },
      to: { win: "sdk-context", side: "top" },
      media: "(width > 900px)",
    },
    {
      from: { win: "sdk-context", side: "right" },
      to: { win: "sdk-result", side: "left", at: 0.7 },
      media: "(width > 900px)",
    },
    {
      from: { win: "sdk-context", side: "bottom" },
      to: { win: "sdk-result", side: "top", at: 0.7 },
      media: "(width <= 900px)",
    },
  ],
  mobileRoutes: [
    {
      from: { win: "sdk-request", side: "bottom" },
      to: { win: "sdk-context", side: "top" },
      media: "(width <= 600px)",
    },
    {
      from: { win: "sdk-context", side: "bottom" },
      to: { win: "sdk-result", side: "top", at: 0.7 },
    },
  ],
};

export const learningCase = {
  routes: [
    {
      from: { win: "course-brand", side: "bottom" },
      to: { win: "course-prompt", side: "top", at: 0.06 },
    },
    {
      from: { win: "course-prompt", side: "bottom", at: 0.92 },
      to: { win: "course-context", side: "top", at: 0.92 },
    },
    {
      from: { win: "course-context", side: "bottom", at: 0.08 },
      to: { win: "course-results", side: "top", at: 0.08 },
    },
    {
      from: { win: "course-results", side: "bottom", at: 0.92 },
      to: { win: "course-review", side: "top", at: 0.92 },
    },
  ],
};

export const appHero = {
  routes: [
    {
      from: { win: "app-hero", side: "bottom" },
      to: { win: "app-bridge", side: "top" },
    },
  ],
};

export const appWorkspace = {
  routes: [
    {
      from: { win: "app-host", side: "right", at: 0.45 },
      to: { win: "app-workspace", side: "left", at: 0.5 },
      media: "(width > 1100px)",
    },
    {
      from: { win: "app-workspace", side: "right", at: 0.65 },
      to: { win: "app-remote", side: "left", at: 0.45 },
      media: "(width > 1100px)",
    },
    {
      from: { win: "app-workspace", side: "bottom", at: 0.25 },
      to: { win: "app-host", side: "top" },
      media: "(width <= 1100px)",
    },
    {
      from: { win: "app-workspace", side: "bottom", at: 0.8 },
      to: { win: "app-remote", side: "top" },
      media: "(width <= 1100px)",
    },
  ],
  mobileRoutes: [
    {
      from: { win: "app-workspace", side: "bottom", at: 0.25 },
      to: { win: "app-host", side: "top" },
      media: "(width > 600px)",
    },
    {
      from: { win: "app-workspace", side: "bottom", at: 0.8 },
      to: { win: "app-remote", side: "top" },
      media: "(width > 600px)",
    },
    {
      from: { win: "app-host", side: "bottom" },
      to: { win: "app-workspace", side: "top" },
      media: "(width <= 600px)",
    },
    {
      from: { win: "app-workspace", side: "bottom" },
      to: { win: "app-remote", side: "top" },
      media: "(width <= 600px)",
    },
  ],
};

export const appDownloads = {
  routes: [
    {
      from: { win: "download-heading", side: "right", at: 0.6 },
      to: { win: "download-art", side: "left" },
    },
    {
      from: { win: "download-heading", side: "bottom", at: 0.4 },
      to: { win: "download-mac", side: "top" },
    },
    {
      from: { win: "download-art", side: "bottom" },
      to: { win: "download-ios", side: "top" },
    },
  ],
  mobileRoutes: [
    {
      from: { win: "download-art", side: "top" },
      steps: [
        ["n", { to: { win: "download-heading", side: "bottom" }, axis: "y" }],
      ],
    },
    {
      from: { win: "download-art", side: "bottom" },
      steps: [["s", { to: { win: "download-mac", side: "top" }, axis: "y" }]],
    },
    {
      from: { win: "download-mac", side: "bottom", at: 0.3 },
      to: { win: "download-ios", side: "top", at: 0.3 },
    },
  ],
};

export const cloudCosts = {
  routes: ["runtime", "computer", "agent"].map((part) => ({
    from: { win: "cost-heading", side: "bottom" },
    to: { win: `cost-${part}`, side: "top" },
  })),
  mobileRoutes: [
    {
      from: { win: "cost-heading", side: "bottom", at: 0.35 },
      to: { win: "cost-runtime", side: "top", at: 0.5 },
    },
    {
      from: { win: "cost-runtime", side: "bottom", at: 0.7 },
      to: { win: "cost-computer", side: "top", at: 0.7 },
    },
    {
      from: { win: "cost-computer", side: "bottom", at: 0.3 },
      to: { win: "cost-agent", side: "top", at: 0.3 },
    },
  ],
};

export const cloudMachines = {
  tone: "dark",
  routes: [
    {
      from: { win: "cloud-product", side: "right" },
      to: { win: "cloud-provider", side: "left" },
    },
    {
      from: { win: "cloud-product", side: "bottom", at: 0.64 },
      to: { win: "cloud-art", side: "top" },
    },
    ...[0, 1, 2].flatMap((index) => [
      {
        from: { win: "cloud-provider", side: "right" },
        to: { win: `cloud-machine-${index}`, side: "left" },
        media: "(width >= 1024px)",
      },
      {
        from: { win: "cloud-provider", side: "bottom" },
        to: { win: `cloud-machine-${index}`, side: "top" },
        media: "(width < 1024px)",
      },
    ]),
  ],
  mobileRoutes: [
    {
      from: { win: "cloud-product", side: "bottom" },
      to: { win: "cloud-provider", side: "top" },
    },
    ...[0, 1, 2].map((index) => ({
      from: { win: "cloud-provider", side: "bottom" },
      to: { win: `cloud-machine-${index}`, side: "top" },
    })),
  ],
};

export const questions = {
  routes: [
    {
      from: { win: "questions-intro", side: "right", at: 0.4 },
      to: { win: "questions-list", side: "left", at: 0.35 },
    },
  ],
  mobileRoutes: [
    {
      from: { win: "questions-intro", side: "bottom", at: 0.35 },
      to: { win: "questions-list", side: "top", at: 0.5 },
    },
  ],
};

export const sdkRuntime = {
  routes: [
    {
      from: { win: "sdk-product", side: "bottom", at: 0.5 },
      to: { win: "sdk-runtime", side: "top", at: 0.5 },
    },
    ...["codex", "claude", "antigravity"].map((agent) => ({
      from: { win: "sdk-runtime", side: "bottom" },
      to: { win: `sdk-${agent}`, side: "top" },
    })),
  ],
};

export const closing = {
  routes: [
    {
      from: {
        x: "50%",
        y: 0,
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "closing-cta",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "cta-dock",
        side: "top",
        at: 1,
        offset: -108,
      },
      steps: [
        [
          "nw",
          {
            to: {
              win: "closing-cta",
              side: "top",
              offset: 383,
            },
            axis: "x",
          },
        ],
        [
          "n",
          {
            to: {
              x: "50%",
              y: 323,
            },
          },
        ],
        ["nw", 101],
        ["n", 36],
        ["nw", 122],
        ["w", 96],
        ["nw", 64],
      ],
      media: "(width >= 1280px)",
    },
  ],
  overlay: "(width >= 1280px)",
};

export const connect = {
  routes: [
    {
      from: { win: "connect-intro", side: "right", at: 0.55 },
      to: { win: "connect-code", side: "left", at: 0.55 },
      media: "(width >= 1280px)",
    },
    {
      from: { win: "connect-code", side: "bottom" },
      to: { x: "50%", y: "100%" },
      media: "(width >= 1280px)",
    },
    {
      from: {
        x: "50%",
        y: "100%",
      },
      steps: [
        [
          "n",
          {
            to: {
              win: "connect-code",
              side: "bottom",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "connect-intro",
        side: "bottom",
        part: 1,
        at: 0.85,
      },
      to: {
        win: "connect-code",
        side: "top",
        at: 0.12,
      },
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "connect-texture",
        side: "top",
      },
      to: {
        win: "connect-intro",
        side: "bottom",
        part: 1,
        at: 0.4,
      },
    },
  ],
  mobileRoutes: [
    {
      from: {
        win: "connect-intro",
        side: "bottom",
        part: 1,
        at: 0.75,
      },
      to: {
        win: "connect-code",
        side: "top",
        at: 0.75,
      },
    },
    {
      from: {
        x: "50%",
        y: "100%",
      },
      steps: [
        [
          "n",
          {
            to: {
              win: "connect-code",
              side: "bottom",
            },
          },
        ],
      ],
    },
  ],
};

export const diagram = {
  routes: [
    {
      from: {
        win: "hiw-files",
        side: "right",
      },
      sharp: [1, 2],
      steps: [
        [
          "e",
          {
            to: {
              win: "hiw-figma",
              side: "top",
              at: 0.72,
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "hiw-rb",
              side: "left",
              offset: -64,
            },
          },
        ],
        [
          "se",
          {
            to: {
              win: "hiw-rb",
              side: "left",
            },
          },
        ],
      ],
      media: "(width >= 1024px)",
    },
    {
      from: {
        win: "hiw-web",
        side: "right",
      },
      steps: [
        [
          "e",
          {
            to: {
              win: "hiw-figma",
              side: "top",
              at: 0.685,
            },
          },
        ],
      ],
      media: "(width >= 1024px)",
    },
    {
      from: {
        win: "hiw-figma",
        side: "top",
        at: 0.685,
      },
      steps: [
        [
          "n",
          {
            to: {
              win: "hiw-web",
              side: "right",
            },
          },
        ],
        [
          "n",
          {
            to: {
              win: "hiw-rb",
              side: "left",
              offset: 16,
            },
          },
        ],
        [
          "ne",
          {
            to: {
              win: "hiw-rb",
              side: "left",
            },
          },
        ],
        [
          "e",
          {
            to: {
              win: "hiw-rb",
              side: "left",
            },
          },
        ],
      ],
      media: "(width >= 1024px)",
    },
    {
      from: {
        win: "hiw-rb",
        side: "right",
      },
      steps: [
        [
          "e",
          {
            to: {
              x: "64.5%",
              y: 0,
            },
          },
        ],
      ],
      media: "(width >= 1024px)",
    },
    {
      from: {
        win: "hiw-agents",
        side: "left",
      },
      steps: [
        [
          "w",
          {
            to: {
              x: "64.5%",
              y: 0,
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "hiw-api",
              side: "left",
            },
          },
        ],
        [
          "e",
          {
            to: {
              win: "hiw-api",
              side: "left",
            },
          },
        ],
      ],
      sharp: [1, 2],
      media: "(width >= 1024px)",
    },
    {
      from: {
        x: "50%",
        y: "100%",
      },
      steps: [
        [
          "n",
          {
            to: {
              win: "hiw-hub-foot",
              side: "bottom",
            },
          },
        ],
      ],
      media: "(width >= 1024px)",
    },
    {
      from: {
        win: "hiw-sources",
        side: "bottom",
        at: 0.25,
      },
      steps: [
        ["s", 40],
        [
          "e",
          {
            to: {
              win: "hiw-hub-top",
              side: "top",
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "hiw-hub-top",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1024px)",
    },
    {
      from: {
        win: "hiw-hub-foot",
        side: "bottom",
      },
      steps: [
        ["s", 40],
        [
          "e",
          {
            to: {
              win: "hiw-agents",
              side: "top",
              at: 0.75,
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "hiw-agents",
              side: "top",
              at: 0.75,
            },
          },
        ],
      ],
      media: "(width < 1024px)",
    },
    {
      from: {
        win: "hiw-agents",
        side: "bottom",
        at: 0.75,
      },
      to: {
        win: "hiw-product",
        side: "top",
        at: 0.75,
      },
      media: "(width < 1024px)",
    },
    {
      from: {
        win: "hiw-product",
        side: "bottom",
      },
      to: {
        x: "50%",
        y: "100%",
      },
      media: "(width < 1024px)",
    },
  ],
  tone: "dark",
};

export const footer = {
  routes: [],
  mobileRoutes: [
    {
      from: {
        win: "footer",
        side: "top",
        at: 1,
        offset: -44,
      },
      steps: [
        ["n", 24],
        ["nw", 40],
        ["n", "edge"],
      ],
    },
  ],
};

export const hero = {
  routes: [
    {
      from: {
        win: "hero",
        side: "bottom",
      },
      steps: [["s", "edge"]],
    },
  ],
  tone: "light",
};

export const manifesto = {
  routes: [
    {
      from: {
        x: "50%",
        y: 0,
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "manifesto-tab",
              side: "top",
            },
          },
        ],
      ],
    },
  ],
  tone: "light",
};
