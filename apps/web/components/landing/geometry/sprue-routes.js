/** Reference connector paths, separated from markup and presentation styles. */
export const appHero = {
  routes: [
    {
      from: { win: "app-hero", side: "bottom" },
      to: { win: "app-bridge", side: "top" },
    },
    { from: { win: "app-bridge", side: "bottom" }, steps: [["s", "edge"]] },
  ],
};

export const appDevices = {
  routes: [
    {
      from: { win: "app-computer", side: "right", at: 0.6 },
      to: { win: "app-phone", side: "left", at: 0.45 },
    },
  ],
  mobileRoutes: [
    {
      from: { win: "app-computer", side: "bottom", at: 0.65 },
      to: { win: "app-phone", side: "top", at: 0.5 },
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
      from: {
        x: "75%",
        y: 0,
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "connect-code",
              side: "top",
            },
          },
        ],
      ],
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
        x: "50%",
        y: 0,
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "connect-intro",
              side: "top",
            },
          },
        ],
      ],
    },
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

export const features = {
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
              win: "feat-brief-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width >= 1280px)",
    },
    {
      from: {
        win: "feat-brief-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-generate-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width >= 1280px)",
    },
    {
      from: {
        win: "feat-generate-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-review-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width >= 1280px)",
    },
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
              win: "feat-header",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-header",
        side: "bottom",
      },
      steps: [
        ["s", 28],
        [
          "w",
          {
            to: {
              win: "feat-brief-part",
              side: "top",
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "feat-brief-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-brief-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-generate-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-generate-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-review-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        x: "75%",
        y: "100%",
      },
      steps: [
        ["n", 32],
        [
          "w",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
        [
          "n",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
      ],
      media: "(768px <= width < 1280px)",
    },
    {
      from: {
        x: "75%",
        y: "100%",
      },
      steps: [
        ["n", 32],
        [
          "w",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
        [
          "n",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
      ],
      media: "(width >= 1280px)",
    },
  ],
  mobileRoutes: [
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
              win: "feat-header",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-header",
        side: "bottom",
      },
      steps: [
        ["s", 28],
        [
          "w",
          {
            to: {
              win: "feat-brief-part",
              side: "top",
            },
          },
        ],
        [
          "s",
          {
            to: {
              win: "feat-brief-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-brief-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-generate-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        win: "feat-generate-part",
        side: "bottom",
      },
      steps: [
        [
          "s",
          {
            to: {
              win: "feat-review-part",
              side: "top",
            },
          },
        ],
      ],
      media: "(width < 1280px)",
    },
    {
      from: {
        x: "50%",
        y: "100%",
      },
      steps: [
        ["n", 32],
        [
          "w",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
        [
          "n",
          {
            to: {
              win: "feat-review-part",
              side: "bottom",
            },
          },
        ],
      ],
      media: "(width < 768px)",
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
    {
      from: {
        win: "hero",
        side: "bottom",
        offset: 160,
      },
      steps: [
        ["s", 56],
        ["e", 342],
        ["se", 88],
        ["e", "edge"],
      ],
      media: "(width >= 1280px)",
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
