"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  ArrowUpRight,
  CalendarDays,
  Check,
  Coffee,
  MapPin,
  Pause,
  Play,
  Plus,
  RotateCcw,
  Utensils,
  Wallet,
} from "lucide-react";
import SecretaryPreview from "./SecretaryPreview";
import UseCaseArt from "./UseCaseArt";
import { navigateTabs } from "./tabs";
import styles from "./personal.module.css";

const examples = [
  {
    id: "secretary",
    label: "Personal secretary",
    detail: "A little head start",
    name: "A little ahead",
    prompt: "Help me get my day in order.",
    reply: "A place for your priorities. And a little breathing room.",
    character: "bean",
    art: "calendar",
    icon: CalendarDays,
    slug: "personal-secretary",
  },
  {
    id: "meals",
    label: "Meal planner",
    detail: "Dinner, decided",
    name: "Little kitchen",
    prompt: "Make dinner one less thing to think about.",
    reply: "Your week is on the menu. Swap anything you like.",
    character: "bean",
    art: "plan",
    icon: Utensils,
    slug: "meal-planner",
  },
  {
    id: "trip",
    label: "Trip companion",
    detail: "Room to wander",
    name: "Somewhere good",
    prompt: "Make a little home for our weekend plans.",
    reply: "A few good stops. Plenty of space to find your own.",
    character: "sprout",
    art: "map",
    icon: MapPin,
    slug: "travel-companion",
  },
  {
    id: "spending",
    label: "Spending tracker",
    detail: "The little things, in view",
    name: "Pocket picture",
    prompt: "Help me see where the little things add up.",
    reply: "A clearer picture of the week, one little thing at a time.",
    character: "robot",
    art: "chart",
    icon: Wallet,
    slug: "spending-tracker",
  },
] as const;

const dinners = [
  "Lemon & herb bowl",
  "Roasted tomato pasta",
  "Miso mushroom rice",
];
const places = {
  Lisbon: [
    "A slow morning in Alfama",
    "Lunch by the river",
    "Sunset at the miradouro",
  ],
  Copenhagen: [
    "Coffee in Nørrebro",
    "A walk along the harbor",
    "Dinner with the locals",
  ],
};

export default function PersonalSection({
  audience = "app",
}: {
  audience?: "app" | "developers";
}) {
  const forProducts = audience === "developers";
  const [example, setExample] =
    useState<(typeof examples)[number]["id"]>("secretary");
  const [dinner, setDinner] = useState(0);
  const [city, setCity] = useState<keyof typeof places>("Lisbon");
  const [coffees, setCoffees] = useState(0);
  const [notice, setNotice] = useState("");
  const [autoPlay, setAutoPlay] = useState(true);
  const [hovered, setHovered] = useState(false);
  const [inView, setInView] = useState(false);
  const [motionAllowed, setMotionAllowed] = useState(false);
  const [pageVisible, setPageVisible] = useState(true);
  const scene = useRef<HTMLDivElement>(null);
  const spent = 280 + coffees * 6;

  useEffect(() => {
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const updateMotion = () => setMotionAllowed(!preference.matches);
    const updateVisibility = () => setPageVisible(!document.hidden);
    updateMotion();
    updateVisibility();
    preference.addEventListener("change", updateMotion);
    document.addEventListener("visibilitychange", updateVisibility);
    const observer = new IntersectionObserver(
      ([entry]) =>
        setInView(entry.isIntersecting && entry.intersectionRatio >= 0.45),
      { threshold: [0, 0.45], rootMargin: "-72px 0px -20px 0px" },
    );
    if (scene.current) observer.observe(scene.current);
    return () => {
      observer.disconnect();
      preference.removeEventListener("change", updateMotion);
      document.removeEventListener("visibilitychange", updateVisibility);
    };
  }, []);

  useEffect(() => {
    if (!autoPlay || hovered || !inView || !motionAllowed || !pageVisible)
      return;
    const timer = window.setInterval(() => {
      setExample(
        (current) =>
          examples[
            (examples.findIndex((item) => item.id === current) + 1) %
              examples.length
          ].id,
      );
    }, 6500);
    return () => window.clearInterval(timer);
  }, [autoPlay, hovered, inView, motionAllowed, pageVisible]);

  return (
    <section
      className={styles.personal}
      id="possibilities"
      aria-labelledby="personal-title"
    >
      <div
        className={styles.personalInner}
        onPointerEnter={(event) => {
          if (event.pointerType === "mouse" || event.pointerType === "pen")
            setHovered(true);
        }}
        onPointerLeave={() => setHovered(false)}
        onFocusCapture={(event) => {
          if (!(event.target as HTMLElement).closest("[data-playback]"))
            setAutoPlay(false);
        }}
        onPointerDownCapture={(event) => {
          if (!(event.target as HTMLElement).closest("[data-playback]"))
            setAutoPlay(false);
        }}
      >
        <div className={styles.personalCopy}>
          <h2 id="personal-title">
            Wish it existed?
            <br />
            <em>Let’s make it.</em>
          </h2>
          <p>
            {forProducts
              ? "A planner. A meal guide. A tool for tracking spending. Build useful features around your users’ tasks, with agents doing the work behind them."
              : "A calmer day. A plan for the weekend. That little tool you can’t find. Describe the result you need, then review and refine the work."}
          </p>
          <div
            className={styles.exampleChoices}
            role="tablist"
            aria-label={
              forProducts
                ? "Ideas for your product"
                : "Things you can make in the app"
            }
            onKeyDown={navigateTabs}
          >
            {examples.map((item) => (
              <button
                key={item.id}
                type="button"
                role="tab"
                id={`everyday-tab-${item.id}`}
                aria-label={item.label}
                aria-selected={example === item.id}
                aria-controls={`everyday-example-${item.id}`}
                tabIndex={example === item.id ? 0 : -1}
                data-example={item.id}
                onClick={() => {
                  setAutoPlay(false);
                  setExample(item.id);
                  setNotice("");
                }}
              >
                <UseCaseArt kind={item.art} className={styles.choiceArt} />
                <span>
                  <strong>{item.label}</strong>
                  <small>{item.detail}</small>
                </span>
                <ArrowRight
                  className={styles.choiceArrow}
                  size={15}
                  aria-hidden="true"
                />
              </button>
            ))}
          </div>
          <div className={styles.choiceFooter}>
            <Link
              href={forProducts ? "/sdk" : "/use-cases"}
              className={styles.exploreCases}
            >
              {forProducts ? "Build with the SDK" : "Find your next idea"}
              <ArrowUpRight size={17} />
            </Link>
            {motionAllowed && (
              <button
                type="button"
                data-playback
                className={styles.playback}
                onClick={() => setAutoPlay(!autoPlay)}
                aria-label={
                  autoPlay
                    ? "Pause automatic preview"
                    : "Resume automatic preview"
                }
                title={
                  autoPlay
                    ? "Pause automatic preview"
                    : "Resume automatic preview"
                }
              >
                {autoPlay ? <Pause size={14} /> : <Play size={14} />}
              </button>
            )}
          </div>
        </div>

        <div className={styles.makingScene} ref={scene} data-example={example}>
          {examples.map((item) => (
            <div
              key={item.id}
              id={`everyday-example-${item.id}`}
              role="tabpanel"
              aria-labelledby={`everyday-tab-${item.id}`}
              hidden={example !== item.id}
              className={styles.examplePanel}
              data-example={item.id}
            >
              <div className={styles.conversation}>
                <div className={styles.ideaNote}>
                  <span>You</span>
                  <p>{item.prompt}</p>
                </div>
                <div className={styles.botReply}>
                  <item.icon size={26} strokeWidth={1.5} />
                  <p>{item.reply}</p>
                </div>
              </div>
              <div className={styles.everydayApp}>
                <div className={styles.appTop}>
                  <span className={styles.appIdentity}>
                    <span>
                      <item.icon size={17} />
                    </span>
                    {item.name}
                  </span>
                  <span className={styles.previewLabel}>Made for you</span>
                </div>
                <div className={styles.appContent}>
                  {item.id === "secretary" && (
                    <SecretaryPreview density="comfortable" />
                  )}
                  {item.id === "meals" && (
                    <div className={styles.appBody}>
                      <div className={styles.appHeading}>
                        <div>
                          <h3>A good week on a plate.</h3>
                          <p>Simple dinners. A little less deciding.</p>
                        </div>
                        <span className={styles.weekLabel}>This week</span>
                      </div>
                      <div className={styles.dinnerHero}>
                        <div className={styles.plateArt} aria-hidden="true">
                          <MealArt dinner={dinner} />
                        </div>
                        <div>
                          <small>Monday’s dinner</small>
                          <strong>{dinners[dinner]}</strong>
                          <span>{[20, 25, 30][dinner]} min · made for two</span>
                        </div>
                      </div>
                      <div className={styles.dinnerRow}>
                        <span>Tue</span>
                        <strong>Crispy chickpea wraps</strong>
                        <small>15 min</small>
                      </div>
                      <div className={styles.dinnerRow}>
                        <span>Wed</span>
                        <strong>Roasted vegetable soup</strong>
                        <small>25 min</small>
                      </div>
                      <button
                        className={styles.appAction}
                        type="button"
                        onClick={() => {
                          const next = (dinner + 1) % dinners.length;
                          setDinner(next);
                          setNotice(`Monday’s dinner is now ${dinners[next]}.`);
                        }}
                      >
                        Try another dinner
                        <RotateCcw size={15} />
                      </button>
                    </div>
                  )}
                  {item.id === "trip" && (
                    <div className={styles.appBody}>
                      <div className={styles.appHeading}>
                        <div>
                          <h3>A weekend worth keeping.</h3>
                          <p>A little plan. Plenty of room.</p>
                        </div>
                      </div>
                      <div
                        className={styles.cityChoices}
                        role="group"
                        aria-label="Example destination"
                      >
                        {(
                          Object.keys(places) as Array<keyof typeof places>
                        ).map((place) => (
                          <button
                            key={place}
                            type="button"
                            aria-pressed={city === place}
                            onClick={() => {
                              setCity(place);
                              setNotice(`Showing the ${place} weekend plan.`);
                            }}
                          >
                            <MapPin size={14} />
                            {place}
                          </button>
                        ))}
                      </div>
                      <div className={styles.tripStops}>
                        {places[city].map((place, i) => (
                          <div key={place}>
                            <time>{["10:00", "13:00", "18:00"][i]}</time>
                            <i aria-hidden="true" />
                            <span>
                              <strong>{place}</strong>
                              <small>
                                {
                                  [
                                    "Start slowly",
                                    "Follow your appetite",
                                    "Stay a little longer",
                                  ][i]
                                }
                              </small>
                            </span>
                          </div>
                        ))}
                      </div>
                      <div className={styles.tripFooter}>
                        <Check size={15} />
                        <span>Three good stops. The rest is yours.</span>
                      </div>
                    </div>
                  )}
                  {item.id === "spending" && (
                    <div className={styles.appBody}>
                      <div className={styles.appHeading}>
                        <div>
                          <h3>A little more clarity.</h3>
                          <p>Keep the week in perspective.</p>
                        </div>
                        <span className={styles.weekLabel}>This week</span>
                      </div>
                      <div className={styles.spendingSummary}>
                        <div>
                          <span>Spent this week</span>
                          <strong>
                            ${spent}
                            <small> / $400</small>
                          </strong>
                          <p>${400 - spent} left for the good stuff.</p>
                        </div>
                        <svg
                          viewBox="0 0 100 100"
                          aria-label={`${spent} of 400 dollars in example spending`}
                          role="img"
                        >
                          <circle
                            cx="50"
                            cy="50"
                            r="36"
                            fill="none"
                            stroke="var(--mw-inset)"
                            strokeWidth="9"
                          />
                          <circle
                            cx="50"
                            cy="50"
                            r="36"
                            fill="none"
                            stroke="var(--mw-blue)"
                            strokeWidth="9"
                            strokeDasharray={`${(spent / 400) * 226.2} 226.2`}
                            strokeLinecap="round"
                            transform="rotate(-90 50 50)"
                          />
                          <text
                            x="50"
                            y="55"
                            textAnchor="middle"
                            fill="var(--mw-ink)"
                            fontSize="16"
                          >
                            {Math.round((spent / 400) * 100)}%
                          </text>
                        </svg>
                      </div>
                      <div className={styles.dinnerRow}>
                        <span className={styles.expenseIcon}>
                          <Wallet size={15} />
                        </span>
                        <strong>Everyday essentials</strong>
                        <small>$244</small>
                      </div>
                      <div className={styles.dinnerRow}>
                        <span className={styles.expenseIcon}>
                          <Coffee size={15} />
                        </span>
                        <strong>A few good coffees</strong>
                        <small>${36 + coffees * 6}</small>
                      </div>
                      <button
                        className={styles.appAction}
                        type="button"
                        onClick={() => {
                          const next = coffees === 5 ? 0 : coffees + 1;
                          setCoffees(next);
                          setNotice(
                            next
                              ? `Example coffee added. Weekly spending is ${280 + next * 6} dollars.`
                              : "Example spending reset.",
                          );
                        }}
                      >
                        {coffees === 5
                          ? "Reset example"
                          : "Add an example coffee"}
                        {coffees === 5 ? (
                          <RotateCcw size={15} />
                        ) : (
                          <Plus size={16} />
                        )}
                      </button>
                    </div>
                  )}
                </div>
                <div className={styles.appFooter}>
                  <span>Interactive example</span>
                  <Link
                    href={
                      forProducts
                        ? "/docs/guides/quickstart"
                        : `/use-cases/${item.slug}`
                    }
                  >
                    {forProducts
                      ? "Build a feature like this"
                      : "Make one like this"}
                    <ArrowUpRight size={15} />
                  </Link>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
      <span className="sr-only" role="status">
        {notice}
      </span>
    </section>
  );
}

function MealArt({ dinner }: { dinner: number }) {
  return (
    <svg viewBox="0 0 100 100" fill="none">
      <circle cx="50" cy="50" r="44" fill="var(--mw-surface)" />
      <circle cx="50" cy="50" r="34" stroke="var(--mw-line)" />
      {dinner === 0 ? (
        <>
          <path d="M30 59q-9-31 15-27 19 4 1 26Z" fill="#7EA980" />
          <path d="M47 64q-6-26 15-24 21 5 0 24Z" fill="#A7BD85" />
          <path d="M32 41q17-2 5 28" stroke="#4F815C" strokeWidth="2" />
          <circle cx="66" cy="35" r="8" fill="#E5A55B" />
          <circle cx="63" cy="65" r="8" fill="#D98961" />
          <circle cx="33" cy="66" r="6" fill="#D98961" />
        </>
      ) : dinner === 1 ? (
        <>
          <path
            d="M31 36q41 6 10 15t22 17M36 27q43 14 13 29t-12 17M27 46q20-15 31-5t-17 23"
            stroke="#DFB577"
            strokeWidth="7"
            strokeLinecap="round"
          />
          <circle cx="65" cy="35" r="8" fill="#C96B4B" />
          <circle cx="29" cy="62" r="7" fill="#C96B4B" />
          <path d="M54 64q21-4 9-16-13 1-9 16Z" fill="#7EA980" />
        </>
      ) : (
        <>
          <ellipse cx="49" cy="53" rx="25" ry="21" fill="#E0CC9A" />
          <path
            d="M25 47q1-22 22-13l-2 8Zm28-5q10-20 23-2l-3 8ZM49 65q3-22 22-12l-1 10Z"
            fill="#A4836E"
          />
          <path
            d="m35 38 4 14m25-9-5 11m1 5-3 13"
            stroke="#C8AF95"
            strokeWidth="6"
            strokeLinecap="round"
          />
          <path
            d="m32 58 5 5m17-28 4 5m-10 25 5 4"
            stroke="#7EA980"
            strokeWidth="3"
            strokeLinecap="round"
          />
        </>
      )}
    </svg>
  );
}
