import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { CloudComputer, ComputerPlan, CloudImage } from "@shared/cloud";
import { HttpError, request } from "./api";

export type { CloudImage } from "@shared/cloud";
interface CloudCatalog {
  available: boolean;
  plans: ComputerPlan[];
}
const cloudRequest = <T,>(path: string, method = "GET", body?: unknown) =>
  request<T>(`/api/cloud${path}`, method, body);

function useCloudState() {
  const [computers, setComputers] = useState<CloudComputer[]>([]),
    [catalog, setCatalog] = useState<CloudCatalog>(),
    [images, setImages] = useState<CloudImage[]>([]);
  const [loaded, setLoaded] = useState(false),
    [error, setError] = useState("");
  const refreshing = useRef<Promise<void> | null>(null),
    reconciling = useRef(false);
  const recovery = useRef(new Map<string, { failures: number; nextAt: number }>()),
    manualOperations = useRef(new Set<string>());
  const updateComputer = useCallback((computer: CloudComputer) => {
    setComputers((current) => {
      if (computer.state === "deleted") return current.filter((item) => item.id !== computer.id);
      const previous = current.find((item) => item.id === computer.id);
      if (!previous) return [computer, ...current];
      if (JSON.stringify(previous) === JSON.stringify(computer)) return current;
      return current.map((item) => item.id === computer.id ? computer : item);
    });
  }, []);
  const deferRecovery = useCallback((id: string, error: unknown) => {
    const failures = (recovery.current.get(id)?.failures ?? 0) + 1;
    // Setup/permission/contract failures need an explicit retry after correction.
    // Transport failures back off instead of hammering billing every five seconds.
    const delay = error instanceof HttpError && error.retryable === false
      ? Infinity : Math.min(120000, 15000 * 2 ** Math.min(failures - 1, 3));
    recovery.current.set(id, { failures, nextAt: Date.now() + delay });
  }, []);
  const reload = useCallback(() => {
    if (refreshing.current) return refreshing.current;
    const work = (async () => {
      try {
        const [inventory, pricing] = await Promise.all([
          cloudRequest<{ computers: CloudComputer[] }>("/computers"),
          cloudRequest<CloudCatalog>("/catalog"),
        ]);
        setComputers(inventory.computers);
        setCatalog(pricing);
        setLoaded(true);
        setError("");
      } catch (error) {
        setError((error as Error).message);
        throw error;
      } finally {
        refreshing.current = null;
      }
    })();
    refreshing.current = work;
    return work;
  }, []);
  useEffect(() => {
    void reload().catch(() => {});
  }, [reload]);
  useEffect(() => {
    if (
      !computers.some((c) =>
        [
          "checkout_pending",
          "payment_pending",
          "paid",
          "provisioning",
        ].includes(c.state),
      )
    )
      return;
    const timer = setInterval(async () => {
      if (reconciling.current || document.hidden) return;
      reconciling.current = true;
      try {
        for (const computer of computers.filter((c) =>
          [
            "checkout_pending",
            "payment_pending",
            "paid",
            "provisioning",
          ].includes(c.state),
        )) {
          if (manualOperations.current.has(computer.id) || (recovery.current.get(computer.id)?.nextAt ?? 0) > Date.now()) continue;
          try {
            const result = await cloudRequest<{ computer: CloudComputer }>(`/computers/${computer.id}/reconcile`, "POST");
            recovery.current.delete(computer.id);
            updateComputer(result.computer);
          } catch (error) {
            deferRecovery(computer.id, error);
            setError((error as Error).message);
          }
        }
      } catch (error) {
        setError((error as Error).message);
      } finally {
        reconciling.current = false;
      }
    }, 5000);
    return () => clearInterval(timer);
  }, [computers, deferRecovery, updateComputer]);
  const loadImages = useCallback(async () => {
    const result = await cloudRequest<{ images: CloudImage[] }>(
      "/provider/workspace/images?monthly=true",
    );
    setImages(result.images);
    return result.images;
  }, []);
  const action = useCallback(async (id: string, operation: string) => {
    manualOperations.current.add(id);
    setError("");
    try {
      const result = await cloudRequest<{ computer?: CloudComputer; url?: string }>(
        `/computers/${encodeURIComponent(id)}/${operation}`, "POST",
      );
      recovery.current.delete(id);
      if (result.computer) updateComputer(result.computer);
      if (result.url) window.location.assign(result.url);
      else if (operation === "checkout" && result.computer?.checkout)
        window.location.assign(result.computer.checkout.url);
      return result;
    } catch (error) {
      deferRecovery(id, error);
      // A failed checkout may already be persisted. Read it; never create another
      // purchase or change its idempotency key to recover an uncertain payment.
      try {
        const saved = await cloudRequest<{ computer: CloudComputer }>(`/computers/${encodeURIComponent(id)}`);
        updateComputer(saved.computer);
      } catch { /* Keep the known purchase and the original operation error. */ }
      setError((error as Error).message);
      throw error;
    } finally {
      manualOperations.current.delete(id);
    }
  }, [deferRecovery, updateComputer]);
  const retryRecovery = useCallback(async () => {
    recovery.current.clear();
    await reload();
  }, [reload]);
  async function createComputer(
    name: string,
    imageId: string,
    planId: string,
    key: string,
  ) {
    const result = await cloudRequest<{ computer: CloudComputer }>(
      "/computers",
      "POST",
      { name, imageId, planId, idempotencyKey: key },
    );
    updateComputer(result.computer);
    return result;
  }
  return {
    computers,
    catalog,
    images,
    loaded,
    error,
    reload,
    loadImages,
    action,
    createComputer,
    retryRecovery,
  };
}
const CloudContext = createContext<ReturnType<typeof useCloudState> | null>(
  null,
);
export function CloudProvider({ children }: { children: ReactNode }) {
  return (
    <CloudContext.Provider value={useCloudState()}>
      {children}
    </CloudContext.Provider>
  );
}
export function useCloud() {
  const value = useContext(CloudContext);
  if (!value) throw new Error("CloudProvider required");
  return value;
}
export const computerState = (computer: CloudComputer) =>
  ({
    draft: "Not purchased",
    checkout_pending: "Preparing checkout",
    payment_pending: "Awaiting payment",
    paid: "Payment confirmed",
    provisioning: "Setting up",
    ready: "Available",
    deleting: "Deleting",
    deleted: "Deleted",
  })[computer.state];
