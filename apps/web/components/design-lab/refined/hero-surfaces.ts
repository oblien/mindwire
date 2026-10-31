export const heroSurfaces = {
  glass: {
    image: "/images/design-lab/materials/hero-refracted-glass-v1.webp",
    strength: 0.72,
    anchor: 0.43,
    motion: 0,
  },
  layers: {
    image: "/images/design-lab/materials/hero-optical-layers-v1.webp",
    strength: 0.72,
    anchor: 0.68,
    motion: 1,
  },
} as const;

export type HeroVariant = keyof typeof heroSurfaces;
