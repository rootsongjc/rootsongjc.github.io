import matplotlib.pyplot as plt
import matplotlib

# 设置字体，确保英文显示良好
matplotlib.rcParams['font.family'] = 'sans-serif'

modes = {
    'Sidecar Mode': (2, 5),
    'Ambient Mode': (4, 3.5),
    'Cilium Mesh Mode': (3, 2),
    'gRPC Mode*': (5, 4.5)
}

# 设置象限图的大小和分辨率
plt.figure(figsize=(10, 8), dpi=150)

# 设置坐标轴的范围
plt.axis([-0.1, 6.5, -0.1, 6.5])

# 移除网格线
plt.grid(False)

# 移除坐标轴刻度标签
plt.xticks([])
plt.yticks([])

# 设置坐标轴的标签，并调整间距
plt.xlabel('Efficiency (Low -> High)', labelpad=10)
plt.ylabel('Security (Low -> High)', labelpad=10)

# 绘制各模型的数据点，并在旁边标注模型名称及其特点
for mode, (x, y) in modes.items():
    plt.scatter(x, y)
    plt.text(x, y + 0.1, f"{mode}", fontsize=9, ha='center', va='bottom')

# 绘制箭头（添加到坐标轴）
plt.arrow(0, 0, 6, 0, head_width=0.15, head_length=0.2, fc='k', ec='k')
plt.arrow(0, 0, 0, 6, head_width=0.15, head_length=0.2, fc='k', ec='k')

# 移除图框
plt.box(False)

# 添加标题
# plt.title('Comparisons of Istio Data Plane Deployment Modes', pad=10)

# 调整 subplot 布局，增加左边距和下边距
plt.subplots_adjust(left=0.1, bottom=0.1, top=0.95, right=0.95)

# 保存图表为 SVG 文件
plt.savefig('istio-data-plane-deployment-modes.svg', format='svg')

# 显示图表
plt.show()
