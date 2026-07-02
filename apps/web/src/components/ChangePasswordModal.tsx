import React from 'react';

interface ChangePasswordModalProps {
  show: boolean;
  currentPwd: string;
  newPwd: string;
  confirmPwd: string;
  onCurrentPwdChange: (v: string) => void;
  onNewPwdChange: (v: string) => void;
  onConfirmPwdChange: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  onClose: () => void;
}

export function ChangePasswordModal({
  show,
  currentPwd,
  newPwd,
  confirmPwd,
  onCurrentPwdChange,
  onNewPwdChange,
  onConfirmPwdChange,
  onSubmit,
  onClose,
}: ChangePasswordModalProps) {
  if (!show) {
    return null;
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="password-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span>修改密码</span>
          <button type="button" className="modal-close" onClick={onClose}>
            ×
          </button>
        </div>
        <form onSubmit={onSubmit}>
          <div className="password-modal-fields">
            <div className="field-block">
              <label className="field-label required">当前密码</label>
              <input
                type="password"
                value={currentPwd}
                onChange={(e) => onCurrentPwdChange(e.target.value)}
                placeholder="输入当前登录密码"
                title="填写当前账号正在使用的密码"
                autoComplete="current-password"
              />
            </div>
            <div className="field-block">
              <label className="field-label required">新密码</label>
              <input
                type="password"
                value={newPwd}
                onChange={(e) => onNewPwdChange(e.target.value)}
                placeholder="至少 8 位，建议包含字母和数字"
                title="填写新密码，建议至少 8 位并包含字母和数字"
                autoComplete="new-password"
              />
            </div>
            <div className="field-block">
              <label className="field-label required">确认新密码</label>
              <input
                type="password"
                value={confirmPwd}
                onChange={(e) => onConfirmPwdChange(e.target.value)}
                placeholder="再次输入新密码"
                title="再次输入新密码，需与新密码一致"
                autoComplete="new-password"
              />
            </div>
          </div>
          <div className="action-cell password-modal-actions">
            <button type="submit" className="primary-button">
              保存
            </button>
            <button type="button" className="secondary-button" onClick={onClose}>
              取消
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
