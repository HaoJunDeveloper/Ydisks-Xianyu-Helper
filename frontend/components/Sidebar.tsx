import React, { useEffect, useState } from 'react';
import {
  Bell, Box, ChevronLeft, ChevronRight, CreditCard, ExternalLink, LayoutDashboard,
  GitCommitHorizontal, LogOut, MessageCircleMore, RefreshCw, Settings, ShoppingBag, Users, Zap,
} from 'lucide-react';
import { YdisksBrandIcon } from './YdisksLogo';
import { applyUpdate, checkForUpdate, getUpdateReleases, getUpdateStatus, rollbackUpdate, type UpdateCheck, type UpdateRelease, type UpdateState } from '../services/api';

interface SidebarProps {
  activeTab: string;
  isAdmin?: boolean;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  onNavigate: (tab: string) => void;
  onLogout: () => void;
}

interface BuildInfo {
  version: string;
  commit: string;
}

const compareReleaseVersions = (left: string, right: string): number => {
  const parse = (value: string): number[] | null => {
    const parts = value.trim().replace(/^v/, '').split('.');
    if (parts.length !== 3 || parts.some(part => !/^\d+$/.test(part))) return null;
    return parts.map(Number);
  };
  const leftParts = parse(left);
  const rightParts = parse(right);
  if (!leftParts || !rightParts) return 0;
  for (let index = 0; index < 3; index += 1) {
    if (leftParts[index] !== rightParts[index]) return leftParts[index] > rightParts[index] ? 1 : -1;
  }
  return 0;
};

const Sidebar: React.FC<SidebarProps> = ({
  activeTab, isAdmin = false, collapsed, onToggleCollapsed, onNavigate, onLogout,
}) => {
  const [buildInfo, setBuildInfo] = useState<BuildInfo>({ version: 'dev', commit: 'unknown' });
  const [updateOpen, setUpdateOpen] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<UpdateCheck | null>(null);
  const [releases, setReleases] = useState<UpdateRelease[]>([]);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateMessage, setUpdateMessage] = useState('');
  const [updateState, setUpdateState] = useState<UpdateState | null>(null);
  const [updateRefreshKey, setUpdateRefreshKey] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    fetch('/health', { signal: controller.signal })
      .then(response => response.ok ? response.json() : Promise.reject(new Error('health request failed')))
      .then(data => setBuildInfo({
        version: String(data.version || 'dev'),
        commit: String(data.commit || 'unknown'),
      }))
      .catch(error => {
        if (error instanceof DOMException && error.name === 'AbortError') return;
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!updateOpen || !isAdmin) return;
    setUpdateMessage('');
    Promise.all([checkForUpdate(), getUpdateReleases()])
      .then(([check, releaseResult]) => {
        setUpdateCheck(check);
        setReleases(releaseResult.releases || []);
      })
      .catch(error => setUpdateMessage(error instanceof Error ? error.message : '检查更新失败'));
  }, [updateOpen, isAdmin, updateRefreshKey]);

  useEffect(() => {
    if (!updateOpen || !isAdmin) return;
    let cancelled = false;
    const poll = async () => {
      try {
        const state = await getUpdateStatus();
        if (!cancelled) {
          setUpdateState(state);
          if (state.running) window.setTimeout(poll, 2000);
        }
      } catch {
        // The service may restart during a successful update.
      }
    };
    void poll();
    return () => { cancelled = true; };
  }, [updateOpen, isAdmin, updateRefreshKey]);

  const refreshUpdates = () => setUpdateRefreshKey(value => value + 1);

  const triggerUpdate = async (version?: string, rollback = false) => {
    setUpdateBusy(true);
    setUpdateMessage('正在启动更新任务...');
    try {
      const result = rollback ? await rollbackUpdate(version || '') : await applyUpdate(version);
      if ('running' in result) {
        setUpdateState(result);
        setUpdateMessage(result.message);
      } else {
        setUpdateMessage(result.message);
      }
    } catch (error) {
      setUpdateMessage(error instanceof Error ? error.message : '更新失败');
    } finally {
      setUpdateBusy(false);
    }
  };
  const menuItems = [
    { id: 'dashboard', icon: LayoutDashboard, label: '仪表盘' },
    { id: 'accounts', icon: Users, label: '账号管理' },
    { id: 'chat', icon: MessageCircleMore, label: '在线聊天' },
    { id: 'cards', icon: CreditCard, label: '卡密库存' },
    { id: 'items', icon: Box, label: '商品列表' },
    { id: 'orders', icon: ShoppingBag, label: '订单管理' },
    { id: 'rules', icon: Zap, label: '自动化规则' },
    { id: 'notifications', icon: Bell, label: '通知设置' },
    ...(isAdmin ? [{ id: 'settings', icon: Settings, label: '系统与AI' }] : []),
  ];
  const displayVersion = /^\d+\.\d+\.\d+$/.test(buildInfo.version)
    ? `v${buildInfo.version}`
    : buildInfo.version;

  return (
    <aside className={`fixed inset-y-0 left-0 z-20 flex flex-col border-r border-slate-200/80 bg-white/95 shadow-sidebar backdrop-blur-xl transition-[width] duration-300 ${collapsed ? 'w-16' : 'w-64'}`}>
      <div className={`flex h-20 items-center border-b border-slate-100 ${collapsed ? 'justify-center px-2' : 'gap-3 px-5'}`}>
        <YdisksBrandIcon sizeClass="h-10 w-10" />
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="truncate text-base font-black tracking-tight text-slate-950">Ydisks 闲鱼助手</div>
            <div className="mt-1 text-[10px] font-extrabold uppercase tracking-[0.22em] text-sky-600">Operations</div>
          </div>
        )}
      </div>

      <nav className={`flex-1 space-y-1.5 overflow-y-auto pt-5 ${collapsed ? 'px-2' : 'px-3'}`} aria-label="主导航">
        {menuItems.map((item) => {
          const Icon = item.icon;
          const active = activeTab === item.id;
          return (
            <React.Fragment key={item.id}>
              <button
              type="button"
              title={collapsed ? item.label : undefined}
              aria-label={item.label}
              aria-current={active ? 'page' : undefined}
              onClick={() => onNavigate(item.id)}
              className={`group relative flex h-11 w-full items-center rounded-xl transition-colors ${collapsed ? 'justify-center' : 'gap-3 px-3.5'} ${
                active
                  ? 'bg-brand text-white shadow-brand-active'
                  : 'text-slate-500 hover:bg-slate-100 hover:text-slate-900'
              }`}
            >
              <Icon className={`h-[19px] w-[19px] shrink-0 ${active ? 'text-white' : 'text-slate-400 group-hover:text-slate-700'}`} />
              {!collapsed && <span className="truncate text-sm font-bold">{item.label}</span>}
              {active && !collapsed && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-white/90" />}
              </button>
            </React.Fragment>
          );
        })}
      </nav>

      <div>
        <div className="relative border-y border-slate-100 bg-slate-50/70">
          <button
            type="button"
            onClick={() => isAdmin && setUpdateOpen(current => !current)}
            title={isAdmin ? '打开版本更新' : `版本 ${buildInfo.version}`}
            aria-label={isAdmin ? '打开版本更新' : `版本 ${buildInfo.version}`}
            className={`w-full py-2.5 text-left ${collapsed ? 'flex justify-center px-1' : 'px-6'} ${isAdmin ? 'cursor-pointer hover:bg-slate-100' : 'cursor-default'}`}
          >
            {collapsed ? (
              <GitCommitHorizontal className="h-[18px] w-[18px] text-slate-400" />
            ) : (
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-[10px] font-extrabold uppercase tracking-[0.18em] text-slate-400">
                  <GitCommitHorizontal className="h-3.5 w-3.5" />
                  当前版本
                </div>
                <div className="mt-1 flex items-baseline gap-2">
                  <span className="truncate font-mono text-xs font-bold text-slate-700">{displayVersion}</span>
                  <span className="truncate font-mono text-[10px] text-slate-400">{buildInfo.commit}</span>
                </div>
              </div>
            )}
          </button>
          {updateOpen && !collapsed && isAdmin && (
            <div className="border-t border-slate-200 bg-white px-4 py-3 shadow-lg">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-slate-700">版本更新</span>
                <button type="button" title="重新检查" aria-label="重新检查" onClick={refreshUpdates} className="text-slate-400 hover:text-slate-700">
                  <RefreshCw className="h-4 w-4" />
                </button>
              </div>
              <div className="mt-2 text-xs text-slate-500">
                {updateCheck?.update_available ? `最新版本 v${updateCheck.latest_version}` : '已是最新版本'}
              </div>
              {updateCheck?.update_available && (
                <button type="button" disabled={updateBusy} onClick={() => triggerUpdate(updateCheck.latest_version)} className="mt-3 flex h-8 w-full items-center justify-center gap-2 rounded-lg bg-brand text-xs font-bold text-white disabled:opacity-50">
                  <RefreshCw className="h-3.5 w-3.5" />立即更新
                </button>
              )}
              {updateCheck?.release_notes && <details className="mt-3 text-xs text-slate-500"><summary className="cursor-pointer font-bold">查看更新日志</summary><p className="mt-2 max-h-24 overflow-auto whitespace-pre-wrap">{updateCheck.release_notes}</p></details>}
              {updateCheck?.release_url && <a href={updateCheck.release_url} target="_blank" rel="noreferrer" className="mt-3 flex items-center gap-1 text-xs font-bold text-sky-600"><ExternalLink className="h-3.5 w-3.5" />查看发布</a>}
              {releases.length > 1 && (
                <select className="mt-3 h-8 w-full rounded-lg border border-slate-200 px-2 text-xs" defaultValue="" onChange={event => event.target.value && triggerUpdate(event.target.value, true)} disabled={updateBusy}>
                  <option value="">版本回退</option>
                  {releases.filter(release => compareReleaseVersions(release.tag_name, buildInfo.version) < 0)
                    .map(release => <option key={release.tag_name} value={release.tag_name}>{release.tag_name}</option>)}
                </select>
              )}
              {updateState?.running && <p className="mt-2 text-[11px] text-amber-600">更新任务执行中</p>}
              {updateState && !updateState.running && updateState.status !== 'completed' && updateState.status !== '' && (
                <p className="mt-2 text-[11px] text-red-600">{updateState.message}</p>
              )}
              {updateState?.status === 'completed' && <p className="mt-2 text-[11px] text-emerald-600">更新命令已完成，服务可能正在重启</p>}
              {updateMessage && <p className="mt-2 text-[11px] text-slate-500">{updateMessage}</p>}
            </div>
          )}
        </div>
        <div className={`space-y-2 p-2 ${collapsed ? '' : 'p-3'}`}>
          <button
            type="button"
            onClick={onToggleCollapsed}
            title={collapsed ? '展开侧边栏' : '收起侧边栏'}
            aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
            className={`flex h-10 w-full items-center rounded-xl text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 ${collapsed ? 'justify-center' : 'gap-3 px-3'}`}
          >
            {collapsed ? <ChevronRight className="h-5 w-5" /> : <ChevronLeft className="h-5 w-5" />}
            {!collapsed && <span className="text-sm font-bold">收起侧边栏</span>}
          </button>
          <button
            type="button"
            onClick={onLogout}
            title={collapsed ? '退出登录' : undefined}
            aria-label="退出登录"
            className={`flex h-10 w-full items-center rounded-xl text-slate-500 transition-colors hover:bg-red-50 hover:text-red-600 ${collapsed ? 'justify-center' : 'gap-3 px-3'}`}
          >
            <LogOut className="h-5 w-5" />
            {!collapsed && <span className="text-sm font-bold">退出登录</span>}
          </button>
        </div>
      </div>
    </aside>
  );
};

export default Sidebar;
