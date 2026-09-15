"use client";

// 带二次确认的提交按钮：包在 <form action={serverAction}> 里使用，
// 点取消时阻止提交。删除类操作统一走它，避免误点丢数据。
export function ConfirmSubmit({
  message,
  className,
  children,
}: {
  message: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="submit"
      className={
        className ?? "text-sm font-medium text-destructive hover:underline"
      }
      onClick={(e) => {
        if (!window.confirm(message)) {
          e.preventDefault();
        }
      }}
    >
      {children}
    </button>
  );
}
