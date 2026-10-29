// Usherr Neu: Lucide's icons (ISC licence) in place of the app's own set, by
// the same names. Icon.tsx and KindGlyph.tsx ask here first when Neu is on.
import type { LucideIcon } from 'lucide-react'
import {
  Activity, ArrowRight, ArrowUpDown, Ban, BookHeadphones, BookOpen, Bookmark, Bug, Calendar, Camera, Captions, Check,
  ChevronDown, ChevronLeft, ChevronRight, ChevronUp, CircleCheckBig, Clapperboard, Clock, Cloud, Code, Compass, Copy,
  Dices, Disc3, Download, Drama, Ellipsis, ExternalLink, Eye, EyeOff, Film, FlaskConical, Folder, Globe, GripVertical,
  HardDrive, Headphones, Heart, Image, Info, Key, Layers, LayoutGrid, List, ListPlus, LogOut, Magnet, Mail, Menu,
  MessageSquare, MicVocal, Monitor, Moon, Music, Pause, Pencil, Play, Plus, RefreshCw, Repeat, Search, Server, Share2,
  Shield, Shuffle, SkipBack, SkipForward, SlidersHorizontal, Sparkles, Square, Star, Sun, ThumbsDown, ThumbsUp, Trash2,
  TriangleAlert, Trophy, Tv, User, Volume2, VolumeOff, VolumeX, X,
} from 'lucide-react'
import { neuOn } from './look'

const ICONS: Record<string, LucideIcon> = {
  search: Search, trakt: CircleCheckBig, sort: ArrowUpDown, eye: Eye, 'eye-off': EyeOff, trash: Trash2, open: ArrowRight,
  refresh: RefreshCw, plus: Plus, x: X, check: Check, download: Download, film: Film, tv: Tv, 'volume-x': VolumeX,
  folder: Folder, warning: TriangleAlert, info: Info, clock: Clock, ban: Ban, chat: MessageSquare, captions: Captions,
  calendar: Calendar, grid: LayoutGrid, compass: Compass, bookmark: Bookmark, activity: Activity,
  sliders: SlidersHorizontal, list: List, image: Image, more: Ellipsis, hard: HardDrive, sun: Sun, moon: Moon,
  monitor: Monitor, user: User, logout: LogOut, star: Star, external: ExternalLink, flask: FlaskConical, mail: Mail,
  shield: Shield, 'thumb-up': ThumbsUp, 'thumb-down': ThumbsDown, heart: Heart, globe: Globe, server: Server,
  cloud: Cloud, magnet: Magnet, menu: Menu, sparkles: Sparkles, key: Key, play: Play, pause: Pause, stop: Square,
  'skip-prev': SkipBack, 'skip-next': SkipForward, volume: Volume2, 'volume-off': VolumeOff, shuffle: Shuffle,
  dice: Dices, repeat: Repeat, 'chevron-right': ChevronRight, 'chevron-left': ChevronLeft,
  'chevron-down': ChevronDown, 'chevron-up': ChevronUp, music: Music, lyrics: MicVocal, 'playlist-add': ListPlus,
  stack: Layers, edit: Pencil, grip: GripVertical, disc: Disc3, headphones: Headphones, book: BookOpen, share: Share2,
  bug: Bug, camera: Camera, copy: Copy, trophy: Trophy, github: Code,
}

// The media kinds (KindGlyph's movie, tv, music, book, audiobook, comic).
const KINDS: Record<string, LucideIcon> = {
  movie: Clapperboard, tv: Tv, music: Disc3, book: BookOpen, audiobook: BookHeadphones, comic: Drama,
}

export function neuIcon(name: string): LucideIcon | null {
  return neuOn() ? (ICONS[name] ?? null) : null
}

export function neuKind(kind: string): LucideIcon | null {
  return neuOn() ? (KINDS[kind] ?? null) : null
}
