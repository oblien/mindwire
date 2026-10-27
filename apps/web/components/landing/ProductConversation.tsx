"use client";

import { useEffect, useId, useRef, useState } from "react";
import { ArrowRight, ArrowUp, BookOpen, Check, ChevronRight, FileText, Headphones, LoaderCircle, Music2, Plus, RotateCcw, Volume2, VolumeX } from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import { navigateTabs } from "./tabs";
import styles from "./product-conversation.module.css";

const agents = [{ id: "claude", name: "Claude Code" }, { id: "codex", name: "Codex" }, { id: "opencode", name: "OpenCode" }] as const;
const courseSteps = ["Read your piano notes", "Build the playable lessons", "Save the course in your library"];
const quizSteps = ["Use the course material", "Create three practice questions", "Add the quiz to your course"];
const whiteKeys = [
  { id: "C4", name: "C", midi: 60 }, { id: "D4", name: "D", midi: 62 },
  { id: "E4", name: "E", midi: 64 }, { id: "F4", name: "F", midi: 65 },
  { id: "G4", name: "G", midi: 67 }, { id: "A4", name: "A", midi: 69 },
  { id: "B4", name: "B", midi: 71 }, { id: "C5", name: "C", midi: 72 },
];
const blackKeys = [
  { id: "Cs4", name: "C♯", midi: 61, left: 9 },
  { id: "Ds4", name: "D♯", midi: 63, left: 21.5 },
  { id: "Fs4", name: "F♯", midi: 66, left: 46.5 },
  { id: "Gs4", name: "G♯", midi: 68, left: 59 },
  { id: "As4", name: "A♯", midi: 70, left: 71.5 },
];
const allKeys = [...whiteKeys, ...blackKeys];
const questions = [
  { title: "Which notes make a C major chord?", choices: ["C, E, and G", "C, D, and F", "D, F, and A"], correct: 0, explanation: "C, E, and G are the root, third, and fifth of C major." },
  { title: "What is the black key just to the right of C?", choices: ["D♯", "C♯", "C♭"], correct: 1, explanation: "The black key immediately to the right of C is C sharp." },
  { title: "How many notes make a basic triad?", choices: ["Two", "Four", "Three"], correct: 2, explanation: "A triad is a chord made from three notes." },
];
type Phase = "ready" | "course" | "quiz";
type Lesson = "notes" | "chords" | "quiz";

export default function ProductConversation({ active = true }: { active?: boolean }) {
  const id = useId();
  const [phase, setPhase] = useState<Phase>("ready");
  const [step, setStep] = useState(3);
  const [hasCourse, setHasCourse] = useState(true);
  const [hasQuiz, setHasQuiz] = useState(false);
  const [library, setLibrary] = useState(false);
  const [lesson, setLesson] = useState<Lesson>("notes");
  const [agent, setAgent] = useState<(typeof agents)[number]>(agents[0]);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [followup, setFollowup] = useState<{ request: string; response: string } | null>(null);
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [sound, setSound] = useState(true);
  const [audioNotice, setAudioNotice] = useState("");
  const [question, setQuestion] = useState(0);
  const [choice, setChoice] = useState<number | null>(null);
  const [answers, setAnswers] = useState<number[]>([]);
  const audio = useRef<AudioContext | null>(null);
  const transcript = useRef<HTMLDivElement>(null);
  const busy = phase !== "ready";
  const steps = phase === "quiz" ? quizSteps : courseSteps;
  const chordNames = new Set(selectedKeys.map(key => allKeys.find(note => note.id === key)!.name));
  const correctChord = selectedKeys.length === 3 && ["C", "E", "G"].every(note => chordNames.has(note));

  useEffect(() => {
    if (!busy) return;
    const timer = window.setTimeout(() => {
      if (step < 2) setStep(value => value + 1);
      else {
        setPhase("ready");
        setStep(3);
        if (phase === "course") setHasCourse(true);
        else {
          setHasQuiz(true);
          setLesson("quiz");
          setFollowup(current => current && ({ ...current, response: "Added a three-question quiz to your course. Try it here, and check your answers as you go." }));
        }
      }
    }, 850);
    return () => window.clearTimeout(timer);
  }, [busy, phase, step]);

  useEffect(() => {
    if (transcript.current) transcript.current.scrollTop = transcript.current.scrollHeight;
  }, [phase, step, followup]);

  useEffect(() => () => { void audio.current?.close().catch(() => {}); }, []);

  useEffect(() => {
    if (!active && audio.current) {
      const context = audio.current;
      audio.current = null;
      void context.close().catch(() => {});
    }
  }, [active]);

  const resetQuiz = () => { setQuestion(0); setChoice(null); setAnswers([]); };
  const rebuild = (nextAgent = agent) => {
    setAgent(nextAgent);
    setHasQuiz(false);
    setLibrary(false);
    setLesson("notes");
    setSelectedKeys([]);
    setFollowup(null);
    setDraft("");
    setNotice("");
    resetQuiz();
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setHasCourse(reduced);
    setPhase(reduced ? "ready" : "course");
    setStep(reduced ? 3 : 0);
  };
  const addQuiz = (request = "Add a practice quiz to this course.") => {
    setLibrary(false);
    setNotice("");
    setDraft("");
    if (hasQuiz) {
      setLesson("quiz");
      setFollowup({ request, response: "Your practice quiz is ready in the course. Open it whenever you want to try again." });
      return;
    }
    resetQuiz();
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    setFollowup({ request, response: reduced ? "Added a three-question quiz to your course. Try it here, and check your answers as you go." : "I’m turning the lesson material into a short practice quiz." });
    setPhase(reduced ? "ready" : "quiz");
    setStep(reduced ? 3 : 0);
    if (reduced) { setHasQuiz(true); setLesson("quiz"); }
  };
  const submit = () => {
    const request = draft.trim();
    if (!request || busy) return;
    if (/\b(quiz|test|questions)\b/i.test(request)) addQuiz(request);
    else if (/\b(chords?|keyboard|notes)\b/i.test(request)) {
      setLesson(/\bchords?\b/i.test(request) ? "chords" : "notes");
      setLibrary(false);
      setSelectedKeys([]);
      setFollowup({ request, response: "Opened the lesson in your course. The keyboard works here—try the notes yourself." });
      setDraft(""); setNotice("");
    } else setNotice("Try “add a quiz” or “open the chord lesson” in this interactive demo.");
  };

  const play = async (keys: string[]) => {
    if (!sound) return;
    try {
      if (!window.AudioContext) { setAudioNotice("Sound is unavailable here. You can still use the keys."); return; }
      audio.current ??= new AudioContext();
      const context = audio.current;
      await context.resume();
      if (context.state === "closed") return;
      for (const key of keys) {
        const note = allKeys.find(item => item.id === key)!;
        const oscillator = context.createOscillator();
        const gain = context.createGain();
        const now = context.currentTime;
        oscillator.type = "triangle";
        oscillator.frequency.value = 440 * Math.pow(2, (note.midi - 69) / 12);
        gain.gain.setValueAtTime(0, now);
        gain.gain.linearRampToValueAtTime(.045, now + .02);
        gain.gain.exponentialRampToValueAtTime(.0001, now + .7);
        oscillator.connect(gain); gain.connect(context.destination);
        oscillator.start(now); oscillator.stop(now + .75);
        oscillator.onended = () => { oscillator.disconnect(); gain.disconnect(); };
      }
    } catch { setAudioNotice("Sound is unavailable here. You can still use the keys."); }
  };
  const pressKey = (key: string) => {
    setSelectedKeys(current => lesson === "chords" ? current.includes(key) ? current.filter(item => item !== key) : [...current, key] : [key]);
    void play([key]);
  };
  const openLesson = (next: Lesson) => { setLesson(next); setSelectedKeys([]); setLibrary(false); };
  const finished = question === questions.length;
  const checked = answers.length > question;
  const score = answers.filter((answer, index) => answer === questions[index].correct).length;

  return <div className={styles.demo}>
    <div className={styles.product}>
      <header className={styles.topbar}>
        <div className={styles.brand}><span><Music2 size={25} aria-hidden /></span><strong>tempo</strong></div>
        <span className={styles.productType}>Learning studio</span>
        <button type="button" disabled={!hasCourse || busy} onClick={() => setLibrary(value => !value)}><BookOpen size={18} aria-hidden />{library ? "Back to lesson" : "Course library"}<ChevronRight size={17} aria-hidden /></button>
      </header>

      <div className={styles.body}>
        <aside className={styles.chat} aria-label="Agent building a course inside your product">
          <div className={styles.chatHeader}><h3>Make something worth learning.</h3><button type="button" disabled={busy} onClick={() => rebuild()} aria-label="Replay course creation" title="Replay course creation"><RotateCcw size={19} aria-hidden /></button></div>
          <div className={styles.transcript} ref={transcript}>
            <div className={styles.userMessage}><span>You</span><p>Turn my piano notes into a beginner course. Add a keyboard I can actually play.</p><details><summary><FileText size={18} aria-hidden />piano-notes.md</summary><p>Start with the white keys: C, D, E, F, G, A, B. Introduce sharps, then teach C major using C, E, and G. Let learners try the notes themselves.</p></details></div>
            <div className={styles.response}><span className={styles.author}><Music2 size={18} aria-hidden />Tempo</span><p>{phase === "course" ? "I’m building two short lessons from your notes, with a playable keyboard and a chord exercise." : "Your course is ready. Two short lessons, a playable keyboard, and a chord exercise—all saved in your library."}</p>
              {phase !== "course" && <button className={styles.createdCourse} type="button" onClick={() => { setLibrary(false); openLesson("notes"); }}><span className={styles.courseIcon}><Music2 size={24} aria-hidden /></span><span><strong>First chords</strong><span>{hasQuiz ? "2 lessons + a practice quiz" : "2 interactive lessons"}</span></span><ArrowRight size={19} aria-hidden /></button>}
            </div>
            {followup && <><div className={styles.userMessage}><span>You</span><p>{followup.request}</p></div><div className={styles.response}><span className={styles.author}><Music2 size={18} aria-hidden />Tempo</span><p>{followup.response}</p></div></>}
            {busy && <div className={styles.work} role="status"><LoaderCircle size={18} className={styles.spin} aria-hidden /><span>{steps[step]}…</span></div>}
          </div>
          <div className={styles.composerArea}>
            {!busy && <button className={styles.suggestion} type="button" onClick={() => {
              if (!hasQuiz) { addQuiz(); return; }
              openLesson("chords");
              setFollowup({ request: "Open the chord lesson.", response: "Here’s your chord lesson. Choose C, E, and G on the keyboard to play your first chord." });
              setDraft(""); setNotice("");
            }}><Plus size={17} aria-hidden />{hasQuiz ? "Open the chord lesson" : "Add a practice quiz"}</button>}
            <form className={styles.composer} onSubmit={event => { event.preventDefault(); submit(); }}>
              <label className={styles.srOnly} htmlFor={id + "-prompt"}>Message the example agent</label>
              <textarea id={id + "-prompt"} rows={2} maxLength={400} value={draft} disabled={busy} placeholder="Try “add a quiz”…" onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} />
              <button type="submit" disabled={!draft.trim() || busy} aria-label="Send example message"><ArrowUp size={21} aria-hidden /></button>
            </form>
            {notice && <p className={styles.notice} role="status">{notice}</p>}
          </div>
        </aside>

        <div className={styles.experience} aria-label="Course created inside the example product">
          <div className={styles.experienceBar}><span><BookOpen size={18} aria-hidden />{library ? "Your library" : "First chords"}</span><span>{busy ? <><LoaderCircle size={17} className={styles.spin} aria-hidden />{phase === "quiz" ? "Adding a quiz" : "Creating"}</> : <><Check size={17} aria-hidden />Saved in Tempo</>}</span></div>
          {!hasCourse ? <div className={styles.building}><div className={styles.buildingKeys} aria-hidden><i /><i /><i /><i /><i /></div><h3>A little theory.<br />A lot to play with.</h3><p>Creating your interactive course from the notes.</p><progress value={step + 1} max={3} aria-label="Course creation progress" /></div> : library ? <div className={styles.library}><span className={styles.lessonLabel}>Your courses</span><h3>A good idea.<br />Now something to open.</h3><button type="button" className={styles.libraryCourse} onClick={() => openLesson("notes")}><span className={styles.libraryArtwork} aria-hidden><i /><i /><i /><i /><i /></span><span><strong>First chords</strong><span>{hasQuiz ? "2 lessons + 1 quiz" : "2 interactive lessons"}</span></span><ArrowRight size={22} aria-hidden /></button><p>Created from your notes. Ready to make your own.</p></div> : <>
            <div className={styles.lessons} role="tablist" aria-label="Created course lessons" onKeyDown={navigateTabs}>
              {([{ key: "notes", label: "Meet the keys" }, { key: "chords", label: "Make a chord" }, ...(hasQuiz ? [{ key: "quiz", label: "Practice quiz" }] : [])] as { key: Lesson; label: string }[]).map(item => <button type="button" role="tab" key={item.key} id={id + "-" + item.key} aria-controls={id + "-lesson"} aria-selected={lesson === item.key} tabIndex={lesson === item.key ? 0 : -1} onClick={() => openLesson(item.key)}>{item.label}{item.key === "quiz" && <Check size={15} aria-hidden />}</button>)}
            </div>
            <div className={styles.lesson} role="tabpanel" id={id + "-lesson"} aria-labelledby={id + "-" + lesson}>
              {lesson !== "quiz" ? <>
                <span className={styles.lessonLabel}>{lesson === "notes" ? "Your first piano lesson" : "Three notes. One good sound."}</span>
                <h3>{lesson === "notes" ? <>A little theory.<br /><em>A lot of music.</em></> : <>Make your<br /><em>first chord.</em></>}</h3>
                <p className={styles.lessonDescription}>{lesson === "notes" ? "Every song starts somewhere. Try a few keys, then put your first three notes together." : "Choose C, E, and G on the keyboard. Play them together and you’ve made a C major chord."}</p>
                <div className={styles.keyboard} role="group" aria-label="Playable piano keyboard">
                  <div className={styles.whiteKeys}>{whiteKeys.map(key => <button type="button" key={key.id} aria-label={"Play " + key.id} aria-pressed={selectedKeys.includes(key.id)} onClick={() => pressKey(key.id)}><span>{key.name}</span></button>)}</div>
                  {blackKeys.map(key => <button type="button" key={key.id} className={styles.blackKey} style={{ left: key.left + "%" }} aria-label={"Play " + key.name + "4"} aria-pressed={selectedKeys.includes(key.id)} onClick={() => pressKey(key.id)}><span>{key.name}</span></button>)}
                </div>
                <div className={styles.keyboardCaption}><span>{lesson === "chords" ? "Choose your three notes" : "Tap a key. Hear a note."}</span><button type="button" aria-pressed={sound} onClick={() => setSound(value => !value)}>{sound ? <Volume2 size={18} aria-hidden /> : <VolumeX size={18} aria-hidden />}{sound ? "Sound on" : "Sound off"}</button></div>
                <div className={styles.lessonFooter}>
                  <span role="status">{lesson === "chords" ? correctChord ? <><Check size={19} aria-hidden />That’s C major. You made a chord.</> : selectedKeys.length ? selectedKeys.map(key => allKeys.find(note => note.id === key)!.name).join(" + ") : "C + E + G = your first chord" : selectedKeys.length ? "You played " + allKeys.find(note => note.id === selectedKeys[0])!.name + "." : <><Headphones size={19} aria-hidden />Learn it by playing it.</>}</span>
                  <button type="button" onClick={() => lesson === "notes" ? openLesson("chords") : (setSelectedKeys(["C4", "E4", "G4"]), void play(["C4", "E4", "G4"]))}>{lesson === "notes" ? "Make a chord" : "Hear C major"}<ArrowRight size={18} aria-hidden /></button>
                </div>
                {audioNotice && <p className={styles.audioNotice} role="status">{audioNotice}</p>}
              </> : finished ? <div className={styles.quizComplete}><span className={styles.completeMark}><Check size={34} aria-hidden /></span><span className={styles.lessonLabel}>Practice complete</span><h3>{score === 3 ? <>You’ve got<br /><em>the right notes.</em></> : <>A little practice.<br /><em>A little progress.</em></>}</h3><p>{score} of 3 correct. Your course is here whenever you want to try again.</p><button type="button" onClick={resetQuiz}>Try the quiz again <RotateCcw size={18} aria-hidden /></button></div> : <div className={styles.quiz}>
                <span className={styles.lessonLabel}>Practice {question + 1} of {questions.length}</span><h3>{questions[question].title}</h3>
                <fieldset><legend className={styles.srOnly}>{questions[question].title}</legend>{questions[question].choices.map((answer, index) => <label key={answer} data-selected={choice === index} data-correct={checked && index === questions[question].correct}><input type="radio" name={id + "-answer"} value={index} checked={choice === index} disabled={checked} onChange={() => setChoice(index)} /><span>{answer}</span>{checked && index === questions[question].correct && <Check size={18} aria-hidden />}</label>)}</fieldset>
                {checked && <p className={styles.answerFeedback} role="status"><strong>{answers[question] === questions[question].correct ? "That’s right." : "Almost."}</strong> {questions[question].explanation}</p>}
                <button className={styles.quizNext} type="button" disabled={choice === null} onClick={() => checked ? (setQuestion(value => value + 1), setChoice(null)) : setAnswers(current => [...current, choice!])}>{checked ? question === questions.length - 1 ? "See my result" : "Next question" : "Check answer"}<ArrowRight size={18} aria-hidden /></button>
              </div>}
            </div>
          </>}
        </div>
      </div>
    </div>
    <div className={styles.caption}><div className={styles.agentChoices} role="group" aria-label="Choose the example course agent"><span>Built with</span>{agents.map(item => <button type="button" key={item.id} aria-pressed={agent.id === item.id} disabled={busy} onClick={() => rebuild(item)}><AgentLogo agent={item.id} size={21} />{item.name}</button>)}<span>through Mindwire</span></div><p>Interactive example. Scripted agent responses.</p></div>
  </div>;
}
