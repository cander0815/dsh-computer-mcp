using System;
using System.IO;
using System.Runtime.InteropServices;

// 使用系统回收站，并在删除前回调中拒绝无法回收的操作。
[ComImport, Guid("43826D1E-E718-42EE-BC55-A1E261C37BFE"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface TrashShellItem {}

[ComImport, Guid("947AAB5F-0A5C-4C13-B4D6-4BF7836FC9F8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface TrashFileOperation {
 [PreserveSig] int Advise(IntPtr sink, out uint cookie);
 [PreserveSig] int Unadvise(uint cookie);
 [PreserveSig] int SetOperationFlags(uint flags);
 [PreserveSig] int SetProgressMessage([MarshalAs(UnmanagedType.LPWStr)] string message);
 [PreserveSig] int SetProgressDialog(IntPtr dialog);
 [PreserveSig] int SetProperties(IntPtr properties);
 [PreserveSig] int SetOwnerWindow(IntPtr window);
 [PreserveSig] int ApplyPropertiesToItem(IntPtr item);
 [PreserveSig] int ApplyPropertiesToItems(IntPtr items);
 [PreserveSig] int RenameItem(IntPtr item, [MarshalAs(UnmanagedType.LPWStr)] string name, IntPtr sink);
 [PreserveSig] int RenameItems(IntPtr items, [MarshalAs(UnmanagedType.LPWStr)] string name);
 [PreserveSig] int MoveItem(IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name, IntPtr sink);
 [PreserveSig] int MoveItems(IntPtr items, IntPtr destination);
 [PreserveSig] int CopyItem(IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name, IntPtr sink);
 [PreserveSig] int CopyItems(IntPtr items, IntPtr destination);
 [PreserveSig] int DeleteItem(TrashShellItem item, [MarshalAs(UnmanagedType.Interface)] TrashProgressSink sink);
 [PreserveSig] int DeleteItems(IntPtr items);
 [PreserveSig] int NewItem(IntPtr destination, uint attributes, [MarshalAs(UnmanagedType.LPWStr)] string name, [MarshalAs(UnmanagedType.LPWStr)] string template, IntPtr sink);
 [PreserveSig] int PerformOperations();
 [PreserveSig] int GetAnyOperationsAborted([MarshalAs(UnmanagedType.Bool)] out bool aborted);
}
[ComVisible(true), Guid("04B0F1A7-9490-44BC-96E1-4296A31252E2"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface TrashProgressSink {
 [PreserveSig] int StartOperations();
 [PreserveSig] int FinishOperations(int result);
 [PreserveSig] int PreRenameItem(uint flags, IntPtr item, [MarshalAs(UnmanagedType.LPWStr)] string name);
 [PreserveSig] int PostRenameItem(uint flags, IntPtr item, [MarshalAs(UnmanagedType.LPWStr)] string name, int result, IntPtr created);
 [PreserveSig] int PreMoveItem(uint flags, IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name);
 [PreserveSig] int PostMoveItem(uint flags, IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name, int result, IntPtr created);
 [PreserveSig] int PreCopyItem(uint flags, IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name);
 [PreserveSig] int PostCopyItem(uint flags, IntPtr item, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name, int result, IntPtr created);
 [PreserveSig] int PreDeleteItem(uint flags, IntPtr item);
 [PreserveSig] int PostDeleteItem(uint flags, IntPtr item, int result, IntPtr created);
 [PreserveSig] int PreNewItem(uint flags, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name);
 [PreserveSig] int PostNewItem(uint flags, IntPtr destination, [MarshalAs(UnmanagedType.LPWStr)] string name, [MarshalAs(UnmanagedType.LPWStr)] string template, uint attributes, int result, IntPtr created);
 [PreserveSig] int UpdateProgress(uint total, uint completed);
 [PreserveSig] int ResetTimer();
 [PreserveSig] int PauseTimer();
 [PreserveSig] int ResumeTimer();
}
[ComVisible(true), ClassInterface(ClassInterfaceType.None)]
public sealed class SafeTrashSink : TrashProgressSink {
 public bool Recycled;
 public bool Refused;
 public int StartOperations(){return 0;}
 public int FinishOperations(int result){return 0;}
 public int PreRenameItem(uint f,IntPtr i,string n){return 0;}
 public int PostRenameItem(uint f,IntPtr i,string n,int r,IntPtr c){return 0;}
 public int PreMoveItem(uint f,IntPtr i,IntPtr d,string n){return 0;}
 public int PostMoveItem(uint f,IntPtr i,IntPtr d,string n,int r,IntPtr c){return 0;}
 public int PreCopyItem(uint f,IntPtr i,IntPtr d,string n){return 0;}
 public int PostCopyItem(uint f,IntPtr i,IntPtr d,string n,int r,IntPtr c){return 0;}
 public int PreDeleteItem(uint flags,IntPtr item){
  // 没有系统回收标记就终止整个操作，不允许静默永久删除。
  if((flags & 0x80)==0){Refused=true;return unchecked((int)0x80004005);}
  return 0;
 }
 public int PostDeleteItem(uint flags,IntPtr item,int result,IntPtr created){Recycled=(result>=0 && (flags&0x80)!=0 && created!=IntPtr.Zero);return 0;}
 public int PreNewItem(uint f,IntPtr d,string n){return 0;}
 public int PostNewItem(uint f,IntPtr d,string n,string t,uint a,int r,IntPtr c){return 0;}
 public int UpdateProgress(uint t,uint c){return 0;}
 public int ResetTimer(){return 0;}
 public int PauseTimer(){return 0;}
 public int ResumeTimer(){return 0;}
}
public static class SafeTrash {
 [DllImport("shell32.dll",CharSet=CharSet.Unicode,PreserveSig=true)]
 static extern int SHCreateItemFromParsingName(string path,IntPtr context,ref Guid iid,out TrashShellItem item);
 static void Check(int result){Marshal.ThrowExceptionForHR(result);}
 public static void Recycle(string path){
  if(new DriveInfo(Path.GetPathRoot(path)).DriveType!=DriveType.Fixed)throw new InvalidOperationException("仅支持本地固定磁盘的回收站。");
  string recovery=Path.Combine(Path.GetDirectoryName(path),".dsh-trash-recovery-"+Guid.NewGuid().ToString("N"));
  bool backedUp=false;
  TrashShellItem item=null;TrashFileOperation operation=null;
  using(FileStream guard=new FileStream(path,FileMode.Open,FileAccess.Read,FileShare.Read|FileShare.Delete)){
  try{
   // 保留恢复副本直到系统确认生成回收站条目，拒绝不可逆的静默退路。
   File.Copy(path,recovery,false);backedUp=true;
   File.SetAccessControl(recovery,File.GetAccessControl(path));
   Guid iid=new Guid("43826D1E-E718-42EE-BC55-A1E261C37BFE");
   Check(SHCreateItemFromParsingName(path,IntPtr.Zero,ref iid,out item));
   operation=(TrashFileOperation)Activator.CreateInstance(Type.GetTypeFromCLSID(new Guid("3AD05575-8857-4850-9277-11B85BDB8E09")));
   // 强制回收、建立撤销记录、遇错终止，不显示交互框和提权提示。
   Check(operation.SetOperationFlags(0x00080000|0x20000000|0x00100000|0x00000400|0x00000004|0x00000010|0x00002000));
   SafeTrashSink sink=new SafeTrashSink();
   Check(operation.DeleteItem(item,sink));
   Check(operation.PerformOperations());
   bool aborted;Check(operation.GetAnyOperationsAborted(out aborted));
   if(aborted||sink.Refused||!sink.Recycled)throw new InvalidOperationException("系统未确认移入回收站，已拒绝永久删除。");
  }catch(Exception failure){
   if(backedUp && !File.Exists(path)){
    try{File.Move(recovery,path);backedUp=false;}
    catch(Exception restore){backedUp=false;throw new IOException("系统未确认回收，恢复副本保留在："+recovery+"；原错误："+failure.Message,restore);}
   }
   throw;
  }finally{
   if(item!=null)Marshal.ReleaseComObject(item);
   if(operation!=null)Marshal.ReleaseComObject(operation);
   // 这里只清理本工具创建的恢复副本，绝不永久删除原始文件。
   if(backedUp && File.Exists(recovery)){
    try{File.SetAttributes(recovery,FileAttributes.Normal);File.Delete(recovery);}catch{}
   }
  }
  }
 }
}
