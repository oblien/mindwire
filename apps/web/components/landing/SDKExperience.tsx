import Link from "next/link";
import { ArrowUpRight, FileText } from "lucide-react";
import CourseDemo from "../examples/CourseDemo";
import SurfaceRoutes from "./SurfaceRoutes";
import { learningPrompt } from "@/lib/learning-example";
import styles from "./sdk-experience.module.css";

export default function SDKExperience() {
  return (
    <SurfaceRoutes
      as="section"
      kind="sdkExperience"
      className={styles.section}
      aria-labelledby="sdk-features-title"
    >
      <div className={styles.intro}>
        <h2 id="sdk-features-title">
          A lesson brief.
          <br />A whole learning experience.
        </h2>
        <div>
          <p>
            Your user asks for a course. The agent reads their notes, builds the
            interactive lesson, and saves it through your tools. All inside your
            product.
          </p>
          <Link href="/use-cases/learning-studio">
            Walk through the example <ArrowUpRight size={16} />
          </Link>
        </div>
      </div>
      <div className={styles.story}>
        <div className={styles.inputs}>
          <div className={styles.request} data-sprue="sdk-request">
            <h3>
              <span>1</span> Your user asks
            </h3>
            <blockquote>{learningPrompt}</blockquote>
            <div className={styles.file}>
              <FileText size={18} /> piano-notes.md
            </div>
          </div>
          <div className={styles.context} data-sprue="sdk-context">
            <h3>
              <span>2</span> Your product supplies
            </h3>
            <dl>
              <div>
                <dt>Knowledge</dt>
                <dd>Lesson notes & teaching guidelines</dd>
              </div>
              <div>
                <dt>Tools</dt>
                <dd>Read notes, save courses, update the library</dd>
              </div>
              <div>
                <dt>Direction</dt>
                <dd>Make it playable. Keep it beginner-friendly.</dd>
              </div>
            </dl>
            <Link href="/docs/guides/mcp">
              Connect your tools <ArrowUpRight size={15} />
            </Link>
          </div>
        </div>
        <div className={styles.result} data-sprue="sdk-result">
          <div className={styles.resultHeading}>
            <h3>
              <span>3</span> They get something they can use
            </h3>
            <span>Example product</span>
          </div>
          <CourseDemo compact />
          <div className={styles.resultFoot}>
            <span>Play the keys. Build a chord. Try the quiz.</span>
            <Link href="/use-cases/learning-studio#course-results">
              See the full example <ArrowUpRight size={15} />
            </Link>
          </div>
        </div>
      </div>
      <div className={styles.protocol}>
        <span>Build the experience around the work.</span>
        <nav aria-label="SDK capabilities">
          <Link href="/docs/guides/streaming">
            Stream progress <ArrowUpRight size={14} />
          </Link>
          <Link href="/docs/guides/tool-events">
            Show tool activity <ArrowUpRight size={14} />
          </Link>
          <Link href="/docs/guides/interaction">
            Handle approvals <ArrowUpRight size={14} />
          </Link>
        </nav>
      </div>
    </SurfaceRoutes>
  );
}
