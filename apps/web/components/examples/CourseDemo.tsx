"use client";

import { useEffect, useId, useRef, useState } from "react";
import {
  ArrowRight,
  Check,
  Play,
  RotateCcw,
  Volume2,
  VolumeX,
} from "lucide-react";
import { navigateTabs } from "../landing/tabs";
import styles from "./course-demo.module.css";

const keys = [
  { name: "C", frequency: 261.63 },
  { name: "D", frequency: 293.66 },
  { name: "E", frequency: 329.63 },
  { name: "F", frequency: 349.23 },
  { name: "G", frequency: 392 },
  { name: "A", frequency: 440 },
  { name: "B", frequency: 493.88 },
  { name: "C", frequency: 523.25 },
];
const blackKeys = [
  { name: "C sharp", after: 0, frequency: 277.18 },
  { name: "D sharp", after: 1, frequency: 311.13 },
  { name: "F sharp", after: 3, frequency: 369.99 },
  { name: "G sharp", after: 4, frequency: 415.3 },
  { name: "A sharp", after: 5, frequency: 466.16 },
];
const lessons = ["Meet the keys", "Make a chord", "Try the quiz"] as const;
const questions = [
  {
    question: "Which notes make a C major chord?",
    options: ["C, E, G", "C, D, E", "D, F, A"],
    answer: 0,
    explanation:
      "C is the root, E is the major third, and G is the perfect fifth.",
  },
  {
    question: "Which white key comes immediately after C?",
    options: ["B", "D", "F"],
    answer: 1,
    explanation:
      "The white keys follow C, D, E, F, G, A, B, then start again at C.",
  },
];

export default function CourseDemo({ compact = false }: { compact?: boolean }) {
  const id = useId();
  const [lesson, setLesson] = useState(0);
  const [muted, setMuted] = useState(false);
  const [active, setActive] = useState<number[]>([]);
  const [chord, setChord] = useState<number[]>([]);
  const [note, setNote] = useState("Play a key. Hear a note.");
  const [question, setQuestion] = useState(0);
  const [answer, setAnswer] = useState<number | null>(null);
  const [score, setScore] = useState(0);
  const [complete, setComplete] = useState(false);
  const audio = useRef<AudioContext | null>(null);
  const release = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (release.current) clearTimeout(release.current);
      void audio.current?.close();
    },
    [],
  );

  async function sound(frequencies: number[]) {
    if (muted) return;
    try {
      const context = audio.current ?? new AudioContext();
      audio.current = context;
      if (context.state === "suspended") await context.resume();
      frequencies.forEach((frequency) => {
        const oscillator = context.createOscillator();
        const gain = context.createGain();
        oscillator.type = "triangle";
        oscillator.frequency.value = frequency;
        gain.gain.setValueAtTime(0, context.currentTime);
        gain.gain.linearRampToValueAtTime(
          0.16 / frequencies.length,
          context.currentTime + 0.015,
        );
        gain.gain.exponentialRampToValueAtTime(
          0.001,
          context.currentTime + 0.9,
        );
        oscillator.connect(gain);
        gain.connect(context.destination);
        oscillator.start();
        oscillator.stop(context.currentTime + 1);
        oscillator.onended = () => {
          oscillator.disconnect();
          gain.disconnect();
        };
      });
    } catch {
      setNote("Sound isn’t available here. You can still explore the keys.");
    }
  }

  function play(index: number) {
    setNote(`${keys[index].name}${index === 7 ? " · one octave higher" : ""}`);
    void sound([keys[index].frequency]);
    if (lesson === 1)
      setChord((current) =>
        current.includes(index)
          ? current.filter((key) => key !== index)
          : [...current, index],
      );
    else {
      setActive([index]);
      if (release.current) clearTimeout(release.current);
      release.current = setTimeout(() => setActive([]), 450);
    }
  }
  const correctChord =
    chord.length === 3 && [0, 2, 4].every((key) => chord.includes(key));
  const quiz = questions[question];

  return (
    <div className={styles.course} data-compact={compact}>
      <div className={styles.courseBar}>
        <span className={styles.brand}>tempo</span>
        <span>First chords</span>
        <span className={styles.saved}>
          <Check size={15} /> Saved in your library
        </span>
      </div>
      <div
        className={styles.tabs}
        role="tablist"
        aria-label="Piano course lessons"
        onKeyDown={navigateTabs}
      >
        {lessons.map((name, index) => (
          <button
            key={name}
            type="button"
            role="tab"
            id={`${id}-tab-${index}`}
            aria-controls={`${id}-lesson`}
            aria-selected={lesson === index}
            tabIndex={lesson === index ? 0 : -1}
            onClick={() => {
              setLesson(index);
              setChord([]);
              setActive([]);
            }}
          >
            {name}
          </button>
        ))}
      </div>
      <div
        className={styles.lesson}
        role="tabpanel"
        id={`${id}-lesson`}
        aria-labelledby={`${id}-tab-${lesson}`}
        tabIndex={0}
      >
        {lesson < 2 ? (
          <>
            <div className={styles.lessonHeading}>
              <div>
                <span>
                  {lesson === 0
                    ? "Your first piano lesson"
                    : "Three notes, together"}
                </span>
                <h3>
                  {lesson === 0
                    ? "A little theory. A lot of music."
                    : "Make your first chord."}
                </h3>
              </div>
              <button
                type="button"
                className={styles.sound}
                aria-label={muted ? "Turn sound on" : "Mute piano"}
                aria-pressed={!muted}
                onClick={() => setMuted(!muted)}
              >
                {muted ? <VolumeX size={19} /> : <Volume2 size={19} />}
              </button>
            </div>
            <p>
              {lesson === 0
                ? "The white keys repeat from C to B. Try a few, then put your first three notes together."
                : "Select C, E, and G on the keyboard. Listen to each note, then hear them as a C major chord."}
            </p>
            <div className={styles.instrument}>
              <div
                className={styles.keyboard}
                role="group"
                aria-label="Playable piano keyboard"
              >
                {keys.map((key, index) => (
                  <button
                    key={index}
                    type="button"
                    className={styles.whiteKey}
                    aria-label={`Play ${key.name}${index === 7 ? " high" : ""}`}
                    aria-pressed={
                      lesson === 1
                        ? chord.includes(index)
                        : active.includes(index)
                    }
                    onClick={() => play(index)}
                  >
                    <span>{key.name}</span>
                  </button>
                ))}
                {blackKeys.map((key) => (
                  <button
                    key={key.name}
                    type="button"
                    className={styles.blackKey}
                    style={{ left: `${(key.after + 1) * 12.5}%` }}
                    aria-label={`Play ${key.name}`}
                    onClick={() => {
                      setNote(key.name);
                      void sound([key.frequency]);
                    }}
                  />
                ))}
              </div>
              <div className={styles.lessonFoot}>
                <span role="status">
                  {lesson === 1
                    ? correctChord
                      ? "That’s C major. You’ve got it."
                      : chord.length
                        ? `Selected: ${chord.map((key) => keys[key].name).join(" + ")}`
                        : "Start with C, then E and G."
                    : note}
                </span>
                {lesson === 0 ? (
                  <button
                    type="button"
                    onClick={() => {
                      setLesson(1);
                      setChord([]);
                    }}
                  >
                    Make a chord <ArrowRight size={17} />
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setChord([0, 2, 4]);
                      void sound([
                        keys[0].frequency,
                        keys[2].frequency,
                        keys[4].frequency,
                      ]);
                    }}
                  >
                    <Play size={16} /> Play C major
                  </button>
                )}
              </div>
            </div>
          </>
        ) : (
          <div className={styles.quiz}>
            {complete ? (
              <>
                <span>Practice complete</span>
                <h3>
                  {score === 2
                    ? "You know your first chord."
                    : "A good start. Keep playing."}
                </h3>
                <p role="status">
                  {score} of 2 correct. Return to the keyboard whenever you want
                  to practice.
                </p>
                <button
                  type="button"
                  className={styles.quizNext}
                  onClick={() => {
                    setComplete(false);
                    setQuestion(0);
                    setScore(0);
                    setAnswer(null);
                  }}
                >
                  <RotateCcw size={17} /> Try again
                </button>
              </>
            ) : (
              <>
                <span>Question {question + 1} of 2</span>
                <h3>{quiz.question}</h3>
                <div
                  className={styles.answers}
                  role="group"
                  aria-label={quiz.question}
                >
                  {quiz.options.map((option, index) => (
                    <button
                      type="button"
                      key={option}
                      aria-pressed={answer === index}
                      disabled={answer !== null}
                      data-correct={answer !== null && index === quiz.answer}
                      onClick={() => {
                        setAnswer(index);
                        if (index === quiz.answer)
                          setScore((value) => value + 1);
                      }}
                    >
                      {option}
                      {answer !== null && index === quiz.answer && (
                        <Check size={17} />
                      )}
                    </button>
                  ))}
                </div>
                {answer !== null && (
                  <div className={styles.feedback}>
                    <p role="status">
                      {answer === quiz.answer ? "Correct. " : "Almost. "}
                      {quiz.explanation}
                    </p>
                    <button
                      type="button"
                      className={styles.quizNext}
                      onClick={() => {
                        if (question === questions.length - 1)
                          setComplete(true);
                        else {
                          setQuestion(question + 1);
                          setAnswer(null);
                        }
                      }}
                    >
                      {question === questions.length - 1
                        ? "See your result"
                        : "Next question"}
                      <ArrowRight size={17} />
                    </button>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
