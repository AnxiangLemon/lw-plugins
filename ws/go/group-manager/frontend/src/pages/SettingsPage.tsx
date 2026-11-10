import { useState, type FormEvent } from "react";
import {
  BellOff,
  Cable,
  Minus,
  Monitor,
  Moon,
  Plus,
  Save,
  Settings2,
  ShieldCheck,
  Sun,
} from "lucide-react";
import { useAppData } from "@/app/data-context";
import { useTheme, type Theme } from "@/app/theme-provider";
import type { Config } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";

type PreferenceKey = Exclude<keyof Config, "groups">;
const fields: {
  key: PreferenceKey;
  label: string;
  help: string;
  min: number;
  max: number;
  step: number;
  unit: string;
}[] = [
  {
    key: "retention_days",
    label: "日志保留时间",
    help: "超过保留时间的已完成成员日志会自动清理。",
    min: 1,
    max: 3650,
    step: 1,
    unit: "天",
  },
  {
    key: "poll_seconds",
    label: "定时检查间隔",
    help: "0 表示关闭；开启时最少 60 秒，仅检查已开启记录的群。",
    min: 0,
    max: 86400,
    step: 60,
    unit: "秒",
  },
  {
    key: "debounce_ms",
    label: "合并连续变化",
    help: "短时间内收到的重复变化提醒合并为一次查询。",
    min: 100,
    max: 30000,
    step: 100,
    unit: "毫秒",
  },
  {
    key: "cooldown_seconds",
    label: "同群检查最短间隔",
    help: "减少同一个群聊的重复查询。",
    min: 1,
    max: 3600,
    step: 5,
    unit: "秒",
  },
  {
    key: "weak_recheck_seconds",
    label: "账号提醒检查间隔",
    help: "0 表示关闭；收到账号变化提醒后，每轮最多检查五个群。",
    min: 0,
    max: 86400,
    step: 10,
    unit: "秒",
  },
];

/** 草稿只持有用户实际修改的数值，后台刷新不会覆盖输入，也不会保存旧的群配置。 */
export function SettingsPage() {
  const { config, status, loading, error, refresh, persist, notify, navigate } =
    useAppData();
  const { theme, setTheme } = useTheme();
  const [draft, setDraft] = useState<Partial<Record<PreferenceKey, string>>>(
    {},
  );
  const [errors, setErrors] = useState<Partial<Record<PreferenceKey, string>>>(
    {},
  );
  const [saving, setSaving] = useState(false);
  const dirty = Object.keys(draft).length > 0;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!config || saving) return;
    const next = structuredClone(config);
    const invalid: Partial<Record<PreferenceKey, string>> = {};
    for (const field of fields) {
      const raw = draft[field.key] ?? String(config[field.key]);
      const value = Number(raw);
      if (
        !raw.trim() ||
        !Number.isInteger(value) ||
        value < field.min ||
        value > field.max
      )
        invalid[field.key] = `请输入 ${field.min}–${field.max} 之间的整数。`;
      else if (field.key === "poll_seconds" && value > 0 && value < 60)
        invalid[field.key] = "关闭时填写 0，开启时至少 60 秒。";
      else next[field.key] = value;
    }
    setErrors(invalid);
    if (Object.keys(invalid).length) {
      document.getElementById(`setting-${Object.keys(invalid)[0]}`)?.focus();
      return;
    }
    setSaving(true);
    try {
      await persist(next);
      setDraft({});
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : "保存设置失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="偏好设置"
        description="按你的方式管理群聊，数据与配置保存在本机。"
      />
      <section className="rounded-lg border border-border bg-card p-4">
        <div className="mb-4">
          <h2 className="text-base font-semibold">外观</h2>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            选择适合你的工作环境，设置会保存在当前浏览器。
          </p>
        </div>
        <div className="grid max-w-xl grid-cols-3 gap-3">
          {(
            [
              { value: "dark", label: "深色", icon: Moon },
              { value: "light", label: "浅色", icon: Sun },
              { value: "system", label: "跟随系统", icon: Monitor },
            ] satisfies { value: Theme; label: string; icon: typeof Moon }[]
          ).map(({ value, label, icon: Icon }) => (
            <Button
              key={value}
              variant="outline"
              className={`h-12 gap-2 ${theme === value ? "border-highlight/40 bg-highlight/10 text-foreground" : "text-muted-foreground"}`}
              aria-pressed={theme === value}
              onClick={() => setTheme(value)}
            >
              <Icon className="size-4" />
              <span className="text-sm">{label}</span>
            </Button>
          ))}
        </div>
      </section>
      {error && !config ? (
        <ErrorState message={error} onRetry={() => void refresh()} />
      ) : loading && !config ? (
        <LoadingState />
      ) : (
        config && (
          <>
            <section className="flex flex-col justify-between gap-4 rounded-lg border border-border bg-card p-4 sm:flex-row sm:items-center">
              <div className="flex items-start gap-3">
                <div className="rounded-md bg-secondary p-2 text-muted-foreground">
                  <BellOff className="size-5" />
                </div>
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-base font-semibold">记录与消息提醒</h2>
                    <Badge variant="success">默认仅记录</Badge>
                  </div>
                  <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
                    入群与离群默认保存日志。欢迎消息、离群提示可在每个群的设置中单独开启。断线恢复时会补记变化，并跳过过时的消息提醒。
                  </p>
                </div>
              </div>
              <Button variant="outline" onClick={() => navigate("groups")}>
                管理群聊
              </Button>
            </section>
            <form
              className="overflow-hidden rounded-lg border border-border bg-card"
              onSubmit={(event) => void save(event)}
              noValidate
            >
              <div className="flex items-start gap-3 border-b border-border px-4 py-3">
                <Settings2 className="mt-0.5 size-4 text-highlight" />
                <div>
                  <h2 className="text-base font-semibold">同步偏好</h2>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    通常保持默认即可。调整频率只影响已开启记录的群。
                  </p>
                </div>
              </div>
              <div className="divide-y divide-border px-4">
                {fields.map((field) => {
                  const value = draft[field.key] ?? String(config[field.key]);
                  const change = (next: string) => {
                    setDraft((previous) => ({
                      ...previous,
                      [field.key]: next,
                    }));
                    setErrors((previous) => ({
                      ...previous,
                      [field.key]: undefined,
                    }));
                  };
                  const step = (direction: number) => {
                    const number = Number(value);
                    change(
                      String(
                        Math.min(
                          field.max,
                          Math.max(
                            field.min,
                            (Number.isFinite(number)
                              ? number
                              : config[field.key]) +
                              direction * field.step,
                          ),
                        ),
                      ),
                    );
                  };
                  return (
                    <div
                      key={field.key}
                      className="flex flex-col justify-between gap-3 py-3.5 sm:flex-row sm:items-center"
                    >
                      <div className="min-w-0">
                        <label
                          className="text-sm font-medium"
                          htmlFor={`setting-${field.key}`}
                        >
                          {field.label}
                        </label>
                        <p
                          className="mt-1 max-w-xl text-xs leading-5 text-muted-foreground"
                          id={`help-${field.key}`}
                        >
                          {field.help}
                        </p>
                      </div>
                      <div className="w-full shrink-0 sm:w-56">
                        <div className="flex items-center gap-1.5">
                          <Button
                            variant="outline"
                            size="icon"
                            disabled={saving || Number(value) <= field.min}
                            aria-label={`减少${field.label}`}
                            onClick={() => step(-1)}
                          >
                            <Minus className="size-3.5" />
                          </Button>
                          <div className="relative min-w-0 flex-1">
                            <Input
                              id={`setting-${field.key}`}
                              type="number"
                              min={field.min}
                              max={field.max}
                              step={1}
                              inputMode="numeric"
                              disabled={saving}
                              value={value}
                              onChange={(event) => change(event.target.value)}
                              aria-invalid={Boolean(errors[field.key])}
                              aria-describedby={`help-${field.key}${errors[field.key] ? ` error-${field.key}` : ""}`}
                              className="[appearance:textfield] pr-12 tabular-nums [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                            />
                            <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">
                              {field.unit}
                            </span>
                          </div>
                          <Button
                            variant="outline"
                            size="icon"
                            disabled={saving || Number(value) >= field.max}
                            aria-label={`增加${field.label}`}
                            onClick={() => step(1)}
                          >
                            <Plus className="size-3.5" />
                          </Button>
                        </div>
                        {errors[field.key] && (
                          <p
                            id={`error-${field.key}`}
                            className="mt-1.5 text-xs text-destructive"
                            role="alert"
                          >
                            {errors[field.key]}
                          </p>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
              <div className="flex flex-wrap items-center justify-end gap-3 border-t border-border bg-secondary/30 px-4 py-3">
                {dirty && (
                  <span className="mr-auto text-xs text-warning">
                    有未保存的修改
                  </span>
                )}
                <Button
                  variant="ghost"
                  disabled={!dirty || saving}
                  onClick={() => {
                    setDraft({});
                    setErrors({});
                  }}
                >
                  撤销修改
                </Button>
                <Button type="submit" loading={saving}>
                  <Save className="size-4" />
                  保存设置
                </Button>
              </div>
            </form>
            <div className="grid gap-4 xl:grid-cols-2">
              <section className="rounded-lg border border-border bg-card p-4">
                <div className="mb-3 flex items-center gap-2">
                  <Cable className="size-4 text-highlight" />
                  <h2 className="text-base font-semibold">连接与启动</h2>
                  <Badge variant={status?.connected ? "success" : "warning"}>
                    {status?.connected ? "Desktop 已连接" : "等待连接"}
                  </Badge>
                </div>
                <p className="text-sm leading-6 text-muted-foreground">
                  把 Desktop 生成的{" "}
                  <code className="rounded bg-secondary px-1 py-0.5 text-xs text-foreground">
                    plugin-endpoint.json
                  </code>{" "}
                  放在{" "}
                  <code className="rounded bg-secondary px-1 py-0.5 text-xs text-foreground">
                    group-manager.exe
                  </code>{" "}
                  旁边，下次直接运行即可。Desktop
                  重置连接凭据后，更新复制的文件。
                </p>
              </section>
              <section className="rounded-lg border border-border bg-card p-4">
                <div className="mb-3 flex items-center gap-2">
                  <ShieldCheck className="size-4 text-muted-foreground" />
                  <h2 className="text-base font-semibold">本地数据</h2>
                </div>
                <p className="text-sm leading-6 text-muted-foreground">
                  群设置、成员记录与操作结果均保存在程序的数据目录中，重启后会继续保留。替换程序时保留原来的{" "}
                  <code className="rounded bg-secondary px-1 py-0.5 text-xs text-foreground">
                    data
                  </code>{" "}
                  目录即可。
                </p>
              </section>
            </div>
          </>
        )
      )}
    </div>
  );
}
