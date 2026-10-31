import { ArrowUpRight } from "lucide-react";
import CloudCapabilities from "./CloudCapabilities";
import OblienLogo from "@/components/OblienLogo";
import SurfaceRoutes from "./SurfaceRoutes";
import WindowShape from "./WindowShape";
import HeatformArt from "./HeatformArt";
import styles from "./cloud-infrastructure.module.css";

export default function CloudInfrastructure() {
  return (
    <section
      className={styles.infrastructure}
      id="infrastructure"
      aria-labelledby="infrastructure-title"
    >
      <WindowShape kind="diagram" color="var(--mw-inverse)" />
      <div className={styles.intro}>
        <a href="https://oblien.com/" className={styles.provider}>
          <OblienLogo size={30} />
          <span>For business · Oblien</span>
          <ArrowUpRight size={18} />
        </a>
        <h2 id="infrastructure-title">
          Your product.
          <br />
          Room to scale.
        </h2>
        <p>
          Building for your customers? Use the Mindwire SDK with Oblien for
          scalable business infrastructure. Create isolated workspaces through
          its API and manage your fleet directly in Oblien.
        </p>
        <a href="https://oblien.com/" className={styles.textLink}>
          Build on Oblien <ArrowUpRight size={18} />
        </a>
      </div>
      <SurfaceRoutes kind="cloudMachines" className={styles.machines}>
        <div className={styles.sourceColumn}>
          <div className={styles.product} data-sprue="cloud-product">
            <img
              src="/images/framework/api-icon.svg"
              alt=""
              width={34}
              height={34}
              loading="lazy"
            />
            <div>
              <strong>Your product</strong>
              <span>Your customers. Your projects.</span>
            </div>
          </div>
          <div className={styles.sourceArt} data-sprue="cloud-art">
            <HeatformArt variant="cobalt" />
          </div>
        </div>
        <div className={styles.machineBase} data-sprue="cloud-provider">
          <WindowShape kind="diagram" color="var(--mw-diagram-surface)" />
          <img
            src="/images/framework/hub-ticks.svg"
            alt=""
            width={83}
            height={7}
            loading="lazy"
          />
          <OblienLogo size={64} />
          <strong>Oblien</strong>
          <span>Hardware-isolated VMs</span>
        </div>
        <div className={styles.machineGrid}>
          {["Customer A", "Customer B", "Customer C"].map((customer, i) => (
            <div
              className={styles.machine}
              data-sprue={`cloud-machine-${i}`}
              key={customer}
            >
              <div className={styles.machineHeading}>
                <h3>{customer}</h3>
                <span>VM / 0{i + 1}</span>
              </div>
              <ul>
                <li>Own kernel</li>
                <li>Private files</li>
                <li>Own network</li>
              </ul>
            </div>
          ))}
        </div>
      </SurfaceRoutes>
      <p className={styles.diagramCaption}>
        Business infrastructure, managed through Oblien. Separate from the
        personal Mindwire Cloud plans above.
      </p>
      <CloudCapabilities />
      <p className={styles.featureNote}>
        These are Oblien platform capabilities. Availability and pricing follow
        your Oblien account, image, and resource plan. Explore the linked
        documentation for each feature.
      </p>
    </section>
  );
}
